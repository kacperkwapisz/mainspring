package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/route"
)

// Web tools run on the pool the same way requests do. A search goes to the
// account the calling session (named by the same session headers it sends on
// chat requests, e.g. X-Session-ID) is working on, or else the caller's key's
// Claude or Codex session, so it uses the same subscription; otherwise to the
// account the router would pick next. Each call updates limits and shows on the meter.

// webSearcher is implemented by providers that can search the web and answer
// questions on an account (Claude and Codex).
type webSearcher interface {
	WebSearch(ctx context.Context, cred domain.Credential, model, query string, allowed, blocked []string) (provider.WebAnswer, provider.ExecuteResult)
	Ask(ctx context.Context, cred domain.Credential, model, system, prompt string) (provider.WebAnswer, provider.ExecuteResult)
}

// WebResult is a web tool's answer and where it ran.
type WebResult struct {
	provider.WebAnswer
	Provider domain.ProviderID `json:"provider"`
	Model    string            `json:"model"`
}

// ErrNoWebAccount means no Claude or Codex account can take the call.
var ErrNoWebAccount = errors.New("no Claude or Codex account can run web tools right now")

// webProviders are tried in this order when the caller has no session to follow.
var webProviders = []domain.ProviderID{domain.ProviderClaude, domain.ProviderCodex}

// WebSearch searches the web on a pooled account. headers are the caller's
// request headers, used to find its session.
func WebSearch(ctx context.Context, hub *Hub, key domain.LocalKey, headers map[string]string, query string, allowed, blocked []string) (WebResult, error) {
	return runWeb(ctx, hub, key, headers, domain.InboundFormat("mcp.web_search"), func(ex webSearcher, c domain.Credential, model string) (provider.WebAnswer, provider.ExecuteResult) {
		return ex.WebSearch(ctx, c, model, query, allowed, blocked)
	})
}

// AskAboutPage answers a question about page text on a pooled account.
func AskAboutPage(ctx context.Context, hub *Hub, key domain.LocalKey, headers map[string]string, pageURL, page, question string) (WebResult, error) {
	const system = "You answer a question about one web page for another model, which is waiting on the answer. Use only the page content below. Quote exact wording where it matters, and say so plainly if the page doesn't answer the question."
	prompt := "Web page content from " + pageURL + ":\n---\n" + page + "\n---\n\n" + question
	return runWeb(ctx, hub, key, headers, domain.InboundFormat("mcp.web_fetch"), func(ex webSearcher, c domain.Credential, model string) (provider.WebAnswer, provider.ExecuteResult) {
		return ex.Ask(ctx, c, model, system, prompt)
	})
}

type webPlan struct {
	provider   domain.ProviderID
	model      string // empty: pick per account
	sessionKey string
}

func runWeb(ctx context.Context, hub *Hub, key domain.LocalKey, headers map[string]string, inbound domain.InboundFormat, call func(webSearcher, domain.Credential, string) (provider.WebAnswer, provider.ExecuteResult)) (WebResult, error) {
	started := time.Now().UnixMilli()
	var last error = ErrNoWebAccount
	for _, plan := range webPlans(hub, key, route.SessionKey(headers, nil, key.ID)) {
		ex, ok := hub.Executors[plan.provider].(webSearcher)
		if !ok {
			continue
		}
		all, err := hopCredentials(hub, plan.provider, plan.model)
		if err != nil || len(all) == 0 {
			continue
		}
		candidates, _ := hub.router().Order(plan.provider, all, plan.sessionKey)
		for _, cand := range candidates {
			cred := cand.Credential
			model := plan.model
			if model == "" {
				model = webModel(ctx, hub, cred)
			}
			if model == "" || hub.Keys.Allows(key, plan.provider, model) != nil {
				continue
			}
			if nearExpiry(cred) {
				if refreshed, err := refreshCredential(ctx, hub, hub.Executors[plan.provider], cred); err == nil {
					cred = refreshed
				} else if signInRejected(hub, cred, err) {
					continue
				}
			}
			answer, result := call(ex, cred, model)
			observe(hub, cred, result)
			req := Request{Key: key, Inbound: inbound}
			if !result.OK {
				last = fmt.Errorf("%s answered %d: %s", providerLabel(plan.provider), result.Status, result.Message)
				recordEvent(hub, req, plan.provider, model, cred.ID, started, "error", usageCounts{}, "", result.Status, last.Error())
				if result.Retryable || result.Status == 429 || result.Status >= 500 {
					continue
				}
				return WebResult{}, last
			}
			pt, ct, cr, cw := usageFrom(nil, result.Body)
			record(hub, req, plan.provider, model, cred.ID, started, "ok", usageCounts{pt, ct, cr, cw}, "")
			if answer.Text == "" && len(answer.Sources) == 0 {
				return WebResult{}, fmt.Errorf("%s returned nothing for that", providerLabel(plan.provider))
			}
			return WebResult{WebAnswer: answer, Provider: plan.provider, Model: model}, nil
		}
	}
	return WebResult{}, last
}

// webPlans orders where a call may run. The calling session comes first when
// it is a live Claude or Codex session, else the key's newest such session,
// with its model and session key, so the call lands on the account that session
// is bound to. sessionKey is the caller's route.SessionKey; the key-wide
// fallback ("key:<id>") names no particular session.
func webPlans(hub *Hub, key domain.LocalKey, sessionKey string) []webPlan {
	var plans []webPlan
	seen := map[domain.ProviderID]bool{}
	live := hub.router().LiveSessions()
	if sessionKey != "" && sessionKey != "key:"+key.ID {
		for _, s := range live {
			if s.LocalKeyID != key.ID || !webProvider(s.Provider) {
				continue
			}
			if strings.TrimPrefix(s.Key, string(s.Provider)+"|") != sessionKey {
				continue
			}
			seen[s.Provider] = true
			plans = append(plans, webPlan{provider: s.Provider, model: s.Model, sessionKey: sessionKey})
			break
		}
	}
	for _, s := range live { // newest first
		if len(plans) > 0 {
			break // the calling session was found above
		}
		if s.LocalKeyID != key.ID || !webProvider(s.Provider) {
			continue
		}
		seen[s.Provider] = true
		plans = append(plans, webPlan{provider: s.Provider, model: s.Model, sessionKey: strings.TrimPrefix(s.Key, string(s.Provider)+"|")})
	}
	for _, p := range webProviders {
		if !seen[p] {
			plans = append(plans, webPlan{provider: p, sessionKey: "mcp|" + key.ID})
		}
	}
	return plans
}

func webProvider(p domain.ProviderID) bool {
	return p == domain.ProviderClaude || p == domain.ProviderCodex
}

// webModel picks a model for a call with no session to follow: the account's
// smallest Claude line (Haiku) or Codex's own first choice.
func webModel(ctx context.Context, hub *Hub, cred domain.Credential) string {
	var list []domain.ModelInfo
	if live, ok := hub.Executors[cred.Provider].(liveModels); ok {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		list = live.ModelsFor(c, cred)
		cancel()
	}
	if len(list) == 0 {
		list = hub.Executors[cred.Provider].Models()
	}
	if cred.Provider == domain.ProviderClaude {
		for _, m := range list {
			if strings.Contains(m.ID, "haiku") {
				return m.ID
			}
		}
	}
	if len(list) > 0 {
		return list[0].ID
	}
	return ""
}

func providerLabel(p domain.ProviderID) string {
	switch p {
	case domain.ProviderClaude:
		return "Claude"
	case domain.ProviderCodex:
		return "Codex"
	}
	return string(p)
}
