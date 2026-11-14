package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	fcrypto "github.com/kacperkwapisz/mainspring/internal/crypto"
	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
)

// The management API speaks the subset of CLIProxyAPI's /v0/management that
// usage dashboards such as T3 Code read: the account list, a pass-through for
// the providers' usage endpoints, and clearing a cooldown. It is off until a
// management key exists, and api-call only reaches the usage URLs below, so
// the key can read limits but can never spend an account's tokens elsewhere.
var managementURLs = map[string]domain.ProviderID{
	"GET https://chatgpt.com/backend-api/wham/usage":                             domain.ProviderCodex,
	"GET https://chatgpt.com/backend-api/wham/rate-limit-reset-credits":          domain.ProviderCodex,
	"POST https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume": domain.ProviderCodex,
	"GET https://api.anthropic.com/api/oauth/usage":                              domain.ProviderClaude,
}

func managementAuthed(hub *proxy.Hub, r *http.Request) bool {
	if !settingOn(hub, SettingHubEnabled, false) {
		return false
	}
	want, _ := hub.Settings.Get(SettingHubKeyHash)
	if want == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get("X-Management-Key"))
	if got == "" {
		got = strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(got), "bearer ") {
			got = strings.TrimSpace(got[7:])
		}
	}
	return got != "" && fcrypto.SHA256Hex(got) == want
}

func registerManagement(mux *httpx.Mux, hub *proxy.Hub) {
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !managementAuthed(hub, r) {
				httpx.JSON(w, 401, map[string]any{"error": "invalid management key"})
				return
			}
			h(w, r)
		}
	}
	mux.Handle(http.MethodGet, "/v0/management/auth-files", guard(func(w http.ResponseWriter, r *http.Request) {
		creds, _ := hub.Vault.List()
		files := []map[string]any{}
		for _, c := range creds {
			if c.Provider != domain.ProviderClaude && c.Provider != domain.ProviderCodex {
				continue
			}
			st := hub.Router.Get(c.ID)
			f := map[string]any{
				"id": c.ID, "auth_index": c.ID, "name": c.Label, "label": c.Label,
				"type": string(c.Provider), "provider": string(c.Provider),
				"email": c.Tokens.Email, "disabled": st.Benched,
				"status": "active",
			}
			if st.NeedsSignIn {
				f["status"] = "error"
				f["status_message"] = "needs sign-in"
			}
			if c.Provider == domain.ProviderCodex {
				tok := map[string]any{"chatgpt_account_id": c.Tokens.AccountID}
				if st.Plan != "" {
					tok["plan_type"] = strings.ToLower(st.Plan)
				}
				f["id_token"] = tok
			}
			files = append(files, f)
		}
		httpx.JSON(w, 200, map[string]any{"files": files, "observed_at": time.Now().UTC().Format(time.RFC3339)})
	}))
	mux.Handle(http.MethodPost, "/v0/management/api-call", guard(func(w http.ResponseWriter, r *http.Request) {
		body, _ := httpx.ParseBody(r)
		m, _ := body.(map[string]any)
		idx := firstString(m, "auth_index", "authIndex", "AuthIndex")
		method := strings.ToUpper(firstString(m, "method"))
		target := strings.TrimSpace(firstString(m, "url"))
		if method == "" {
			httpx.JSON(w, 400, map[string]any{"error": "missing method"})
			return
		}
		want, ok := managementURLs[method+" "+target]
		if !ok {
			httpx.JSON(w, 403, map[string]any{"error": "Mainspring only forwards the providers' usage endpoints"})
			return
		}
		c, _ := hub.Vault.Get(idx)
		if c == nil || c.Provider != want {
			httpx.JSON(w, 404, map[string]any{"error": "auth not found"})
			return
		}
		cred := *c
		if cred.ExpiresAt != nil && *cred.ExpiresAt-time.Now().UnixMilli() < 5*60*1000 {
			if ex := hub.Executors[cred.Provider]; ex != nil {
				if refreshed, err := hub.Vault.Refresh(r.Context(), cred, ex.Refresh); err == nil {
					cred = refreshed
				}
			}
		}
		headers := map[string]string{}
		if hm, ok := m["header"].(map[string]any); ok {
			for k, v := range hm {
				s, _ := v.(string)
				headers[k] = strings.ReplaceAll(s, "$TOKEN$", cred.Tokens.AccessToken)
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		var res *http.Response
		var err error
		if method == http.MethodGet {
			res, err = provider.GetJSON(ctx, target, headers)
		} else {
			data := strings.ReplaceAll(firstString(m, "data"), "$TOKEN$", cred.Tokens.AccessToken)
			res, err = provider.PostJSON(ctx, target, rawJSON(data), headers)
		}
		if err != nil {
			httpx.JSON(w, 502, map[string]any{"error": "request failed"})
			return
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		header := map[string][]string{}
		for k, v := range res.Header {
			header[k] = v
		}
		httpx.JSON(w, 200, map[string]any{"status_code": res.StatusCode, "header": header, "body": string(raw)})
	}))
	mux.Handle(http.MethodPost, "/v0/management/reset-quota", guard(func(w http.ResponseWriter, r *http.Request) {
		body, _ := httpx.ParseBody(r)
		m, _ := body.(map[string]any)
		idx := firstString(m, "auth_index", "authIndex")
		c, _ := hub.Vault.Get(idx)
		if c == nil {
			httpx.JSON(w, 404, map[string]any{"error": "auth not found"})
			return
		}
		hub.Router.ClearCooldown(c.ID)
		httpx.JSON(w, 200, map[string]any{"status": "ok", "auth_index": c.ID})
	}))
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// rawJSON lets PostJSON send a body that is already JSON text.
type rawJSON string

func (r rawJSON) MarshalJSON() ([]byte, error) {
	if strings.TrimSpace(string(r)) == "" {
		return []byte("{}"), nil
	}
	return []byte(r), nil
}
