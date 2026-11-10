package app

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/oauth"
	"github.com/kacperkwapisz/mainspring/internal/provider/openai"
	"github.com/kacperkwapisz/mainspring/internal/provider/opencode"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

// loginStatus is the outcome of a browser sign-in captured by the callback listener.
type loginStatus struct {
	State   string `json:"state"`
	Done    bool   `json:"done"`
	Account string `json:"account_id,omitempty"`
	Label   string `json:"label,omitempty"`
	Updated bool   `json:"updated,omitempty"` // the account was already in the pool
	Error   string `json:"error,omitempty"`
	At      int64  `json:"-"`
}

var loginResults sync.Map // provider -> loginStatus

// saveLogin stores a finished sign-in, de-duplicating accounts already in the pool.
func saveLogin(hub *proxy.Hub, result oauth.LoginResult) (domain.Credential, bool, error) {
	cred, existed, err := hub.Vault.SaveLogin(store.SaveCredential{
		Provider: result.Provider, Label: result.Label, Tokens: result.Tokens, ExpiresAt: result.ExpiresAt,
	})
	if err != nil {
		return cred, existed, err
	}
	hub.Router.SignedIn(cred.ID)
	// Read the new account's limits (and its email, if the sign-in left it out)
	// now rather than at its first request.
	go func(c domain.Credential) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		proxy.CheckUsage(ctx, hub, c)
	}(cred)
	hub.Activity.Add(store.ActivityAdded, string(cred.Provider), cred.ID, map[string]any{"account": cred.Label, "updated": existed})
	return cred, existed, nil
}

func loginJSON(cred domain.Credential, existed bool) map[string]any {
	return map[string]any{"ok": true, "account_id": cred.ID, "label": cred.Label, "updated": existed, "provider": cred.Provider}
}

func registerLoginAPI(mux *httpx.Mux, hub *proxy.Hub, gate Gate, logins map[domain.ProviderID]oauth.ProviderLogin) {
	authed := func(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !requirePanel(w, r, gate) {
				return
			}
			h(w, r)
		}
	}
	mux.Handle(http.MethodPost, "/api/login/{provider}/start", authed(func(w http.ResponseWriter, r *http.Request) {
		p := domain.ProviderID(httpx.Param(r, "provider"))
		login, ok := logins[p]
		if !ok {
			apiError(w, 404, "unknown_provider", "Mainspring can't sign in to "+string(p))
			return
		}
		body, _ := httpx.ParseBody(r)
		mode := httpx.JSONField(body, "mode")
		start, err := login.Start(r.Context(), "", mode)
		if err != nil {
			apiError(w, 502, "start_failed", err.Error())
			return
		}
		out := map[string]any{"kind": start.Kind, "url": start.URL, "state": start.State, "user_code": start.UserCode, "expires_at": start.ExpiresAt}
		if start.Kind == "code" {
			// The provider shows a code on its own page; the panel posts it to /finish.
			httpx.JSON(w, 200, out)
			return
		}
		if p == domain.ProviderClaude || p == domain.ProviderCodex {
			hint := oauth.ClaudeRedirectURI
			if p == domain.ProviderCodex {
				hint = oauth.CodexRedirectURI
			}
			loginResults.Store(p, loginStatus{State: start.State, At: time.Now().UnixMilli()})
			bind, _ := oauth.BindCallback(p, func(code, state string) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				result, err := login.Complete(ctx, code, state, "", "")
				if err != nil {
					loginResults.Store(p, loginStatus{State: state, Done: true, Error: err.Error()})
					return
				}
				cred, existed, err := saveLogin(hub, result)
				st := loginStatus{State: state, Done: true, Account: cred.ID, Label: cred.Label, Updated: existed}
				if err != nil {
					st.Error = err.Error()
				}
				loginResults.Store(p, st)
			})
			out["kind"] = "callback"
			out["callback"] = hint
			out["listening"] = !bind.Busy
		}
		httpx.JSON(w, 200, out)
	}))
	mux.Handle(http.MethodGet, "/api/login/{provider}/status", authed(func(w http.ResponseWriter, r *http.Request) {
		p := domain.ProviderID(httpx.Param(r, "provider"))
		v, ok := loginResults.Load(p)
		if !ok {
			httpx.JSON(w, 200, loginStatus{})
			return
		}
		st := v.(loginStatus)
		if want := r.URL.Query().Get("state"); want != "" && st.State != "" && st.State != want {
			httpx.JSON(w, 200, loginStatus{State: want})
			return
		}
		httpx.JSON(w, 200, st)
	}))
	mux.Handle(http.MethodPost, "/api/login/{provider}/finish", authed(func(w http.ResponseWriter, r *http.Request) {
		p := domain.ProviderID(httpx.Param(r, "provider"))
		if p != domain.ProviderClaude && p != domain.ProviderCodex {
			apiError(w, 400, "unsupported", "Only Claude and Codex use a callback address")
			return
		}
		body, _ := httpx.ParseBody(r)
		parsed := oauth.ParseCallback(httpx.JSONField(body, "callback"))
		if parsed.Error != "" {
			msg := parsed.ErrorDescription
			if msg == "" {
				msg = parsed.Error
			}
			apiError(w, 400, "provider_refused", msg)
			return
		}
		if parsed.Code == "" {
			apiError(w, 400, "callback_invalid", "Paste the whole address from the tab, starting with http://localhost")
			return
		}
		result, err := logins[p].Complete(r.Context(), parsed.Code, parsed.State, "", "")
		if err != nil {
			apiError(w, 400, "sign_in_failed", err.Error())
			return
		}
		oauth.UnbindCallback(p)
		cred, existed, err := saveLogin(hub, result)
		if err != nil {
			apiError(w, 500, "save_failed", err.Error())
			return
		}
		loginResults.Store(p, loginStatus{State: parsed.State, Done: true, Account: cred.ID, Label: cred.Label, Updated: existed})
		httpx.JSON(w, 200, loginJSON(cred, existed))
	}))
	mux.Handle(http.MethodPost, "/api/login/{provider}/cancel", authed(func(w http.ResponseWriter, r *http.Request) {
		p := domain.ProviderID(httpx.Param(r, "provider"))
		oauth.UnbindCallback(p)
		loginResults.Delete(p)
		httpx.JSON(w, 200, map[string]any{"ok": true})
	}))
	mux.Handle(http.MethodPost, "/api/login/{provider}/device", authed(func(w http.ResponseWriter, r *http.Request) {
		p := domain.ProviderID(httpx.Param(r, "provider"))
		login, ok := logins[p]
		if !ok {
			apiError(w, 404, "unknown_provider", "Mainspring can't sign in to "+string(p))
			return
		}
		body, _ := httpx.ParseBody(r)
		ctx, cancel := context.WithTimeout(r.Context(), 16*time.Minute)
		defer cancel()
		result, err := login.Complete(ctx, "", "", httpx.JSONField(body, "device_code"), "")
		if err != nil {
			apiError(w, 400, "sign_in_failed", err.Error())
			return
		}
		cred, existed, err := saveLogin(hub, result)
		if err != nil {
			apiError(w, 500, "save_failed", err.Error())
			return
		}
		httpx.JSON(w, 200, loginJSON(cred, existed))
	}))
	mux.Handle(http.MethodPost, "/api/login/{provider}/secret", authed(func(w http.ResponseWriter, r *http.Request) {
		p := domain.ProviderID(httpx.Param(r, "provider"))
		body, _ := httpx.ParseBody(r)
		secret := strings.TrimSpace(httpx.JSONField(body, "secret"))
		if secret == "" {
			apiError(w, 400, "secret_missing", "Paste the key first")
			return
		}
		switch p {
		case domain.ProviderCursor:
			result, err := logins[p].Complete(r.Context(), "", "", "", secret)
			if err != nil {
				apiError(w, 400, "key_rejected", "That key was rejected by Cursor. Check it and try again")
				return
			}
			cred, existed, err := saveLogin(hub, result)
			if err != nil {
				apiError(w, 500, "save_failed", err.Error())
				return
			}
			httpx.JSON(w, 200, loginJSON(cred, existed))
		case domain.ProviderOpenCode:
			label, err := opencode.ValidateKey(r.Context(), secret)
			if err != nil {
				apiError(w, 400, "key_rejected", "OpenCode Console rejected that key: "+err.Error())
				return
			}
			cred, existed, err := saveLogin(hub, oauth.LoginResult{
				Provider: domain.ProviderOpenCode, Label: label,
				Tokens: domain.CredentialTokens{AccessToken: secret, Extra: opencode.CredentialExtra()},
			})
			if err != nil {
				apiError(w, 500, "save_failed", err.Error())
				return
			}
			httpx.JSON(w, 200, loginJSON(cred, existed))
		default:
			apiError(w, 400, "unsupported", string(p)+" doesn't sign in with a pasted key")
		}
	}))
	mux.Handle(http.MethodPost, "/api/sources/openai", authed(func(w http.ResponseWriter, r *http.Request) {
		body, _ := httpx.ParseBody(r)
		label := strings.TrimSpace(httpx.JSONField(body, "name"))
		baseURL := strings.TrimSpace(httpx.JSONField(body, "base_url"))
		secret := strings.TrimSpace(httpx.JSONField(body, "key"))
		base, err := openai.NormalizeBaseURL(baseURL)
		if err != nil {
			apiError(w, 400, "base_url_invalid", err.Error())
			return
		}
		host, _, err := openai.Discover(r.Context(), base, secret)
		if err != nil {
			apiError(w, 400, "unreachable", err.Error())
			return
		}
		if label == "" {
			label = host
		}
		existing, _ := hub.Vault.List(domain.ProviderOpenAI)
		slug := openai.UniqueSlug(existing, label)
		cred, err := hub.Vault.Save(store.SaveCredential{
			Provider: domain.ProviderOpenAI, Label: label,
			Tokens: domain.CredentialTokens{AccessToken: secret, Extra: map[string]any{"base_url": base, "slug": slug, "kind": "openai"}},
		})
		if err != nil {
			apiError(w, 500, "save_failed", err.Error())
			return
		}
		hub.Activity.Add(store.ActivityAdded, string(domain.ProviderOpenAI), cred.ID, map[string]any{"account": label})
		httpx.JSON(w, 200, map[string]any{"ok": true, "account_id": cred.ID, "label": label, "slug": slug})
	}))
}
