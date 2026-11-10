package app

import (
	"net/http"
	"strconv"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/auth"
	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/env"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/oauth"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	claudeexec "github.com/kacperkwapisz/mainspring/internal/provider/claude"
	codexexec "github.com/kacperkwapisz/mainspring/internal/provider/codex"
	cursorexec "github.com/kacperkwapisz/mainspring/internal/provider/cursor"
	grokexec "github.com/kacperkwapisz/mainspring/internal/provider/grok"
	openaiexec "github.com/kacperkwapisz/mainspring/internal/provider/openai"
	opencodeexec "github.com/kacperkwapisz/mainspring/internal/provider/opencode"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

type Booted struct {
	Handler http.Handler
	Env     *env.Env
	Hub     *proxy.Hub
	// Gate decides who may use the panel and its JSON API.
	Gate Gate
	// Auth is the panel sign-in service behind Gate.
	Auth *auth.Service
	DB   *db.DB
}

func Create(source map[string]string) (*Booted, error) {
	e, err := env.Load(source)
	if err != nil {
		return nil, err
	}
	d, err := db.Open(e.DatabasePath)
	if err != nil {
		return nil, err
	}
	vault := store.NewVault(d, e.JWTKey)
	keys := store.NewKeyStore(d)
	usage := store.NewUsageStore(d)
	prices, err := store.NewPriceStore(d)
	if err != nil {
		d.Close()
		return nil, err
	}
	settings := store.NewSettingsStore(d)
	router, err := route.New(d)
	if err != nil {
		d.Close()
		return nil, err
	}
	applyRoutingSettings(router, settings)
	activity := store.NewActivityStore(d)
	authSvc := auth.New(e, store.NewWebAuthnStore(d), settings)
	hub := &proxy.Hub{
		Env:      e,
		Vault:    vault,
		Keys:     keys,
		Usage:    usage,
		Prices:   prices,
		Settings: settings,
		Router:   router,
		Activity: activity,
		Executors: map[domain.ProviderID]provider.Executor{
			domain.ProviderClaude:   claudeexec.Executor{ClientID: e.ClaudeClientID},
			domain.ProviderCodex:    codexexec.Executor{ClientID: e.CodexClientID},
			domain.ProviderGrok:     grokexec.Executor{ClientID: e.GrokClientID},
			domain.ProviderCursor:   &cursorexec.Executor{},
			domain.ProviderOpenAI:   openaiexec.NewExecutor(),
			domain.ProviderOpenCode: opencodeexec.NewExecutor(),
		},
	}
	httpx.SetLogLevel(string(e.LogLevel))
	if v, ok := settings.Get(proxy.SettingLogTrace); ok {
		httpx.SetTraceMode(v)
	}
	logins := oauth.Logins(e)
	mux := httpx.NewMux()
	registerHealth(mux)
	registerOpenAI(mux, hub)
	registerAnthropic(mux, hub)
	registerResponses(mux, hub)
	registerResponsesWS(mux, hub)
	registerAuth(mux, authSvc)
	registerPanel(mux)
	registerAPI(mux, hub, authSvc)
	registerLoginAPI(mux, hub, authSvc, logins)
	registerManagement(mux, hub)
	return &Booted{Handler: httpx.Guard(mux), Env: e, Hub: hub, Gate: authSvc, Auth: authSvc, DB: d}, nil
}

// Settings keys owned by the routing engine.
const (
	SettingAffinityTTL = "routing.affinityMinutes"
	SettingStaleAfter  = "routing.staleMinutes"
)

func applyRoutingSettings(r *route.Engine, settings *store.SettingsStore) {
	if v, ok := settings.Get(SettingAffinityTTL); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			r.SetAffinityTTL(time.Duration(n) * time.Minute)
		}
	}
}
