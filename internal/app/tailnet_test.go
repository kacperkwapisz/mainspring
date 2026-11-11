package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/auth"
)

func TestTailnetRequestsNeedMainspringSignIn(t *testing.T) {
	b := bootAPIWith(t, map[string]string{"MAINSPRING_AUTH": "proxy"})
	// Through Umbrel's proxy the panel is open (the proxy vouched)...
	res := httptest.NewRecorder()
	b.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/pool", nil))
	if res.Code != 200 {
		t.Fatalf("proxied: %d", res.Code)
	}
	// ...but the same request over the tailnet isn't.
	strict := auth.Strict(b.Handler)
	res = httptest.NewRecorder()
	strict.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/pool", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("tailnet without a session: %d", res.Code)
	}
	// Its sign-in state says so, and a tailnet device can't pick the address
	// keys are checked against.
	req := httptest.NewRequest(http.MethodGet, "https://mainspring.tail4e2a.ts.net/api/auth/state", nil)
	req.Header.Set("X-Forwarded-Host", "evil.example")
	res = httptest.NewRecorder()
	strict.ServeHTTP(res, req)
	var st map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &st)
	if st["mode"] != "webauthn" || st["signedIn"] != false || st["setupNeeded"] != true || st["rpID"] != "mainspring.tail4e2a.ts.net" {
		t.Fatalf("tailnet auth state: %v", st)
	}
}

func TestTailnetFlowInTheDemo(t *testing.T) {
	b := bootAPIWith(t, map[string]string{"MAINSPRING_AUTH": "proxy"})
	code, st := apiCall(t, b, http.MethodPost, "/api/tailnet/start", map[string]any{"hostname": "Not OK"})
	if code != 400 {
		t.Fatalf("bad name: %d %v", code, st)
	}
	code, st = apiCall(t, b, http.MethodPost, "/api/tailnet/start", map[string]any{"hostname": "mainspring"})
	if code != 200 {
		t.Fatalf("start: %d %v", code, st)
	}
	deadline := time.Now().Add(3 * time.Second)
	for st["state"] != "needs_login" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		_, st = apiCall(t, b, http.MethodGet, "/api/tailnet", nil)
	}
	if st["state"] != "needs_login" || !strings.HasPrefix(st["auth_url"].(string), "https://login.tailscale.com/") {
		t.Fatalf("%v", st)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/tailnet/qr.svg", nil)
	res := httptest.NewRecorder()
	b.Handler.ServeHTTP(res, req)
	if res.Code != 200 || !strings.HasPrefix(res.Body.String(), "<svg") {
		t.Fatalf("qr: %d %.60s", res.Code, res.Body.String())
	}
	if code, _ := apiCall(t, b, http.MethodPost, "/api/tailnet/open", nil); code != 409 {
		t.Fatalf("open before joining: %d", code)
	}
	if v, _ := b.Hub.Settings.Get(SettingTailnetOn); v != "1" {
		t.Fatal("the device should come back after a restart")
	}
	// Leaving during sign-in forgets the device.
	_, st = apiCall(t, b, http.MethodPost, "/api/tailnet/cancel", nil)
	if st["state"] != "off" {
		t.Fatalf("cancel: %v", st)
	}
}

func TestOpenCarriesASetupCodeOnlyWithoutKeys(t *testing.T) {
	b := bootAPIWith(t, map[string]string{"MAINSPRING_AUTH": "proxy"})
	_, _ = apiCall(t, b, http.MethodPost, "/api/tailnet/start", map[string]any{"hostname": "mainspring"})
	deadline := time.Now().Add(15 * time.Second)
	var st map[string]any
	for time.Now().Before(deadline) {
		_, st = apiCall(t, b, http.MethodGet, "/api/tailnet", nil)
		if st["state"] == "connected" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if st["state"] != "connected" || st["url"] != "https://mainspring.tail4e2a.ts.net" {
		t.Fatalf("%v", st)
	}
	_, open := apiCall(t, b, http.MethodPost, "/api/tailnet/open", nil)
	url, _ := open["url"].(string)
	if !strings.HasPrefix(url, "https://mainspring.tail4e2a.ts.net/?setup=") {
		t.Fatalf("no keys yet, so open should carry a setup code: %q", url)
	}
	code := strings.TrimPrefix(url, "https://mainspring.tail4e2a.ts.net/?setup=")
	strict := auth.Strict(b.Handler)
	req := httptest.NewRequest(http.MethodPost, "https://mainspring.tail4e2a.ts.net/api/auth/setup/verify", strings.NewReader(`{"code":"`+code+`"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	strict.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("the code should work on the tailnet address: %d %s", res.Code, res.Body.String())
	}
}
