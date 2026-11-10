package proxy

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/provider/openai"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

var fallbackRouter struct {
	once sync.Once
	e    *route.Engine
}

// router returns the hub's routing engine (an in-memory one when unset, for tests).
func (h *Hub) router() *route.Engine {
	if h.Router != nil {
		return h.Router
	}
	fallbackRouter.once.Do(func() { fallbackRouter.e, _ = route.New(nil) })
	return fallbackRouter.e
}

type usageCounts struct {
	prompt, completion, cacheRead, cacheWrite int64
}

// hopCredentials lists the credentials that can serve model on providerID.
func hopCredentials(hub *Hub, providerID domain.ProviderID, model string) ([]domain.Credential, error) {
	all, err := hub.Vault.List(providerID)
	if err != nil {
		return nil, err
	}
	if providerID == domain.ProviderOpenAI {
		matched := []domain.Credential{}
		for _, c := range all {
			slug := openai.SourceSlug(c)
			if slug != "" && strings.HasPrefix(model, slug+"/") {
				matched = append(matched, c)
			}
		}
		all = matched
	}
	if providerID == domain.ProviderCursor {
		oauth := []domain.Credential{}
		keys := []domain.Credential{}
		for _, c := range all {
			if kind, _ := c.Tokens.Extra["kind"].(string); kind == "api_key" {
				keys = append(keys, c)
			} else {
				oauth = append(oauth, c)
			}
		}
		all = append(oauth, keys...)
	}
	return all, nil
}

func saveRefreshed(hub *Hub, cred domain.Credential) {
	_, _ = hub.Vault.Save(store.SaveCredential{
		ID: cred.ID, Provider: cred.Provider, Label: cred.Label, Tokens: cred.Tokens, ExpiresAt: cred.ExpiresAt,
	})
}

// signInRejected marks a credential as needing a fresh sign-in when the
// provider refused its refresh token, and reports whether it did.
func signInRejected(hub *Hub, cred domain.Credential, err error) bool {
	var re *provider.RefreshError
	if !errors.As(err, &re) || !re.Permanent() {
		return false
	}
	if hub.router().MarkSignIn(cred.ID, re.Error()) && hub.Activity != nil {
		hub.Activity.Add(store.ActivitySignIn, string(cred.Provider), cred.ID, map[string]any{"account": cred.Label})
	}
	return true
}

// observe feeds an upstream response into the router: quota windows from the
// headers, and a cooldown for 429s and repeated auth failures.
func observe(hub *Hub, cred domain.Credential, result provider.ExecuteResult) {
	r := hub.router()
	now := time.Now().UnixMilli()
	switch cred.Provider {
	case domain.ProviderClaude:
		r.ObserveWindows(cred.ID, "headers", route.ClaudeWindows(result.Header))
	case domain.ProviderCodex:
		r.ObserveWindows(cred.ID, "headers", route.CodexWindows(result.Header))
	}
	if result.OK {
		return
	}
	switch result.Status {
	case 429:
		var lim route.Limit
		switch cred.Provider {
		case domain.ProviderClaude:
			lim = route.ClaudeLimit(result.Header, now)
		case domain.ProviderCodex:
			lim = route.CodexLimit(result.Header, result.Raw, now)
		default:
			lim = route.Limit{Until: route.RetryAfter(result.Header, now)}
		}
		before := r.Get(cred.ID)
		until := r.RateLimited(cred.ID, lim)
		if lim.Account && hub.Activity != nil && !before.Cooling(now) {
			hub.Activity.Add(store.ActivityLimit, string(cred.Provider), cred.ID, map[string]any{
				"account": cred.Label, "window": lim.Label, "until": until,
				"sessions": r.SessionsOn(cred.ID),
			})
		}
	case 401, 403:
		r.AuthRejected(cred.ID)
	}
}

// noteMoved adds "Moved N sessions to X" to the limit event that caused it.
func noteMoved(hub *Hub, p domain.ProviderID, fromID string, to domain.Credential) {
	if hub.Activity == nil {
		return
	}
	if ev, ok := hub.Activity.LatestFor(store.ActivityLimit, fromID, 60*60*1000); ok {
		data := ev.Data
		if data == nil {
			data = map[string]any{}
		}
		n, _ := data["moved"].(float64)
		data["moved"] = n + 1
		data["to"] = to.Label
		hub.Activity.Update(ev.ID, data)
		return
	}
	from := fromID
	if c, err := hub.Vault.Get(fromID); err == nil && c != nil {
		from = c.Label
	}
	hub.Activity.Add(store.ActivityMoved, string(p), fromID, map[string]any{"from": from, "to": to.Label, "moved": 1})
}

// movedReason explains why a session left its account.
func movedReason(hub *Hub, fromID string) string {
	st := hub.router().Get(fromID)
	switch {
	case st.NeedsSignIn:
		return "sign-in rejected"
	case st.Benched:
		return "taken out of rotation"
	case st.CoolWindow != "":
		return windowNoun(st.CoolWindow) + " limit hit"
	case st.CoolUntil > time.Now().UnixMilli():
		return "rate limited"
	}
	return "account unavailable"
}

var allOutNoted sync.Map // provider -> unix ms of the last note

func noteAllOut(hub *Hub, p domain.ProviderID, backAt int64) {
	if hub.Activity == nil {
		return
	}
	now := time.Now().UnixMilli()
	if v, ok := allOutNoted.Load(p); ok && now-v.(int64) < 30*60*1000 {
		return
	}
	allOutNoted.Store(p, now)
	hub.Activity.Add(store.ActivityAllOut, string(p), "", map[string]any{"back_at": backAt})
}

var passthroughNames = []string{"X-Codex-Turn-State"}

// passthroughHeaders picks upstream response headers the client needs.
func passthroughHeaders(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	var out http.Header
	for _, name := range passthroughNames {
		if v := h.Get(name); v != "" {
			if out == nil {
				out = http.Header{}
			}
			out.Set(name, v)
		}
	}
	return out
}

// allResting is the answer when every account of a provider is cooling or out.
func allResting(req Request, p domain.ProviderID, model string, backAt int64) Result {
	hint := "add another account, or wait for one to reset"
	header := http.Header{}
	if backAt > 0 {
		wait := (backAt - time.Now().UnixMilli() + 999) / 1000
		if wait < 1 {
			wait = 1
		}
		header.Set("Retry-After", strconv.FormatInt(wait, 10))
		hint = "the first one is back at " + time.UnixMilli(backAt).Format("15:04") + " (in " + humanWait(wait) + ")"
	}
	res := failOut(req, 429, httpx.KindCap, string(p), model, "every "+providerName(p)+" account is resting", hint)
	res.Header = header
	return res
}

func humanWait(sec int64) string {
	switch {
	case sec < 90:
		return strconv.FormatInt(sec, 10) + " s"
	case sec < 90*60:
		return strconv.FormatInt((sec+30)/60, 10) + " min"
	case sec < 36*3600:
		return strconv.FormatInt((sec+1800)/3600, 10) + " h"
	}
	return strconv.FormatInt((sec+43200)/86400, 10) + " d"
}

func providerName(p domain.ProviderID) string {
	switch p {
	case domain.ProviderClaude:
		return "Claude"
	case domain.ProviderCodex:
		return "Codex"
	case domain.ProviderGrok:
		return "Grok"
	case domain.ProviderCursor:
		return "Cursor"
	case domain.ProviderOpenCode:
		return "OpenCode Console"
	case domain.ProviderOpenAI:
		return "OpenAI-compatible"
	}
	return string(p)
}

// InputTokens returns all input tokens of a request, cached or not. Claude
// reports cache reads and writes separately from input_tokens; OpenAI-style
// providers include cached tokens in prompt_tokens already.
func InputTokens(p domain.ProviderID, prompt, cacheRead, cacheWrite int64) int64 {
	if p == domain.ProviderClaude {
		return prompt + cacheRead + cacheWrite
	}
	return prompt + cacheWrite
}

// ServedBy returns which providers would serve a model id, in order, and the
// provider it falls back to on retryable errors (or "").
func ServedBy(hub *Hub, model string) (served []domain.ProviderID, fallback domain.ProviderID) {
	for i, h := range resolveProviderChain(hub, model) {
		served = append(served, h.Provider)
		if i == 1 {
			fallback = h.Provider
		}
	}
	return served, fallback
}

// windowNoun turns a window label into its adjective form: "7 days" becomes
// "7-day", "5 hours" becomes "5-hour". Other labels stay as they are.
func windowNoun(label string) string {
	parts := strings.SplitN(label, " ", 2)
	if len(parts) != 2 {
		return label
	}
	if _, err := strconv.Atoi(parts[0]); err != nil {
		return label
	}
	rest := parts[1]
	for _, unit := range []string{"days", "hours", "minutes", "day", "hour", "minute"} {
		if strings.HasPrefix(rest, unit) {
			return parts[0] + "-" + strings.TrimSuffix(unit, "s") + rest[len(unit):]
		}
	}
	return label
}
