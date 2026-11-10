package app

import (
	"net/http"
	"strconv"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/env"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/oauth"
	"github.com/kacperkwapisz/mainspring/internal/panel"
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
	Panel   *store.PanelAuth
	DB      *db.DB
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
	panelAuth := store.NewPanelAuth(d)
	settings := store.NewSettingsStore(d)
	router, err := route.New(d)
	if err != nil {
		d.Close()
		return nil, err
	}
	applyRoutingSettings(router, settings)
	activity := store.NewActivityStore(d)
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
	registerPanel(mux, hub, e, panelAuth, settings, logins)
	return &Booted{Handler: httpx.Guard(mux), Env: e, Hub: hub, Panel: panelAuth, DB: d}, nil
}

func page(w http.ResponseWriter, body, title, meta string) {
	httpx.HTML(w, http.StatusOK, panel.Layout(title, meta, body))
}

func isSecure(publicURL string) bool {
	return len(publicURL) >= 8 && publicURL[:8] == "https://"
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
