package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/auth/authtest"
)

const testHost = "localhost:8317"
const testOrigin = "http://localhost:8317"

type client struct {
	t       *testing.T
	booted  *Booted
	host    string
	cookies map[string]string
	headers map[string]string
}

func bootAuth(t *testing.T, vars map[string]string) *Booted {
	t.Helper()
	src := map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": filepath.Join(t.TempDir(), "ms.sqlite"), "MAINSPRING_AUTH": "", "MAINSPRING_TRUST_PROXY": "", "PUBLIC_URL": ""}
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

func newClient(t *testing.T, b *Booted) *client {
	return &client{t: t, booted: b, host: testHost, cookies: map[string]string{}, headers: map[string]string{}}
}

func (c *client) do(method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	c.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = c.host
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	parts := []string{}
	for k, v := range c.cookies {
		parts = append(parts, k+"="+v)
	}
	if len(parts) > 0 {
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	res := httptest.NewRecorder()
	c.booted.Handler.ServeHTTP(res, req)
	for _, sc := range res.Result().Cookies() {
		if sc.MaxAge < 0 || sc.Value == "" {
			delete(c.cookies, sc.Name)
		} else {
			c.cookies[sc.Name] = sc.Value
		}
	}
	var out map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res, out
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func expect(t *testing.T, res *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if res.Code != status {
		t.Fatalf("status %d want %d: %s", res.Code, status, res.Body.String())
	}
	if code != "" {
		var body map[string]any
		_ = json.Unmarshal(res.Body.Bytes(), &body)
		if got := errCode(body); got != code {
			t.Fatalf("code %q want %q: %s", got, code, res.Body.String())
		}
	}
}

func setCookieHeader(res *httptest.ResponseRecorder, name string) string {
	for _, v := range res.Header().Values("Set-Cookie") {
		if strings.HasPrefix(v, name+"=") {
			return v
		}
	}
	return ""
}

func TestAuthStateFirstRun(t *testing.T) {
	b := bootAuth(t, nil)
	c := newClient(t, b)
	res, body := c.do(http.MethodGet, "/api/auth/state", "")
	expect(t, res, 200, "")
	want := map[string]any{"mode": "webauthn", "signedIn": false, "setupNeeded": true, "secureContext": true, "httpsURL": "", "rpID": "localhost", "requireKey": false, "keys": float64(0)}
	for k, v := range want {
		if body[k] != v {
			t.Fatalf("%s = %v want %v (%s)", k, body[k], v, res.Body.String())
		}
	}
	c.host = "hub.lan:8317"
	_, body = c.do(http.MethodGet, "/api/auth/state", "")
	if body["secureContext"] != false || body["rpID"] != "hub.lan" {
		t.Fatalf("%v", body)
	}
	res, _ = c.do(http.MethodPost, "/api/auth/login/begin", "")
	expect(t, res, 400, "insecure_context")
}

func TestAuthStateHTTPSURL(t *testing.T) {
	b := bootAuth(t, map[string]string{"PUBLIC_URL": "https://hub.example"})
	_, body := newClient(t, b).do(http.MethodGet, "/api/auth/state", "")
	if body["httpsURL"] != "https://hub.example" {
		t.Fatalf("%v", body)
	}
}

func TestAuthNotUnderV1(t *testing.T) {
	b := bootAuth(t, nil)
	res, _ := newClient(t, b).do(http.MethodGet, "/v1/api/auth/state", "")
	expect(t, res, 404, "")
}

func TestSetupVerify(t *testing.T) {
	b := bootAuth(t, nil)
	code, err := b.Auth.EnsureSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, b)
	res, body := c.do(http.MethodPost, "/api/auth/setup/verify", `{"code":"AAAA-AAAA"}`)
	expect(t, res, 400, "setup_code_wrong")
	if e := body["error"].(map[string]any); e["message"] != "That code doesn't match the one in hub's terminal" {
		t.Fatalf("%v", e)
	}
	if setCookieHeader(res, "ms_setup") != "" {
		t.Fatal("cookie on wrong code")
	}
	res, body = c.do(http.MethodPost, "/api/auth/setup/verify", `{"code":"`+strings.ToLower(strings.ReplaceAll(code, "-", ""))+`"}`)
	expect(t, res, 200, "")
	if body["ok"] != true {
		t.Fatalf("%v", body)
	}
	sc := setCookieHeader(res, "ms_setup")
	if !strings.Contains(sc, "HttpOnly") || !strings.Contains(sc, "SameSite=Lax") || !strings.Contains(sc, "Max-Age=900") {
		t.Fatalf("setup cookie %q", sc)
	}
}

func TestSetupVerifyRateLimit(t *testing.T) {
	b := bootAuth(t, nil)
	code, _ := b.Auth.EnsureSetupCode()
	c := newClient(t, b)
	for i := 0; i < 10; i++ {
		res, _ := c.do(http.MethodPost, "/api/auth/setup/verify", `{"code":"AAAA-AAAA"}`)
		expect(t, res, 400, "setup_code_wrong")
	}
	res, _ := c.do(http.MethodPost, "/api/auth/setup/verify", `{"code":"`+code+`"}`)
	expect(t, res, 429, "setup_code_rate_limited")
}

func TestRegisterAndLoginBeginNeedAuth(t *testing.T) {
	b := bootAuth(t, nil)
	if _, err := b.Auth.EnsureSetupCode(); err != nil {
		t.Fatal(err)
	}
	c := newClient(t, b)
	res, _ := c.do(http.MethodPost, "/api/auth/register/begin", `{"kind":"security-key","name":"YubiKey"}`)
	expect(t, res, 401, "setup_code_required")
	res, _ = c.do(http.MethodPost, "/api/auth/login/begin", "")
	expect(t, res, 409, "no_keys_for_host")
	res, _ = c.do(http.MethodGet, "/api/auth/keys", "")
	expect(t, res, 401, "panel_signed_out")
	c.headers["Authorization"] = "Bearer anything"
	res, _ = c.do(http.MethodGet, "/api/auth/keys", "")
	expect(t, res, 401, "panel_signed_out")
}

func TestBadOriginRefused(t *testing.T) {
	b := bootAuth(t, map[string]string{"MAINSPRING_AUTH": "proxy"})
	c := newClient(t, b)
	c.headers["Origin"] = "https://evil.example"
	res, _ := c.do(http.MethodPut, "/api/auth/require-key", `{"on":false}`)
	expect(t, res, 403, "bad_origin")
	c.headers["Origin"] = testOrigin
	res, _ = c.do(http.MethodPut, "/api/auth/require-key", `{"on":false}`)
	expect(t, res, 200, "")
}

// TestRegisterLoginRoundTrip drives the full ceremony with a software ECDSA
// P-256 authenticator using "none" attestation.
func TestRegisterLoginRoundTrip(t *testing.T) {
	b := bootAuth(t, nil)
	code, err := b.Auth.EnsureSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, b)
	key := authtest.New(testOrigin)

	res, _ := c.do(http.MethodPost, "/api/auth/setup/verify", `{"code":"`+code+`"}`)
	expect(t, res, 200, "")

	// Begin: security-key options are non-discoverable and cross-platform.
	res, opts := c.do(http.MethodPost, "/api/auth/register/begin", `{"kind":"security-key","name":"  YubiKey 5C  "}`)
	expect(t, res, 200, "")
	pk := opts["publicKey"].(map[string]any)
	sel := pk["authenticatorSelection"].(map[string]any)
	if sel["residentKey"] != "discouraged" || sel["requireResidentKey"] != false || sel["authenticatorAttachment"] != "cross-platform" || sel["userVerification"] != "preferred" {
		t.Fatalf("selection %v", sel)
	}
	if hints, _ := pk["hints"].([]any); len(hints) != 1 || hints[0] != "security-key" {
		t.Fatalf("hints %v", pk["hints"])
	}
	if rp := pk["rp"].(map[string]any); rp["id"] != "localhost" || rp["name"] != "Mainspring" {
		t.Fatalf("rp %v", rp)
	}
	if user := pk["user"].(map[string]any); user["name"] != "operator" || user["displayName"] != "Mainspring operator" {
		t.Fatalf("user %v", user)
	}
	if c.cookies["ms_wa"] == "" {
		t.Fatal("no ms_wa cookie")
	}

	att, err := key.Create(res.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	res, body := c.do(http.MethodPost, "/api/auth/register/finish", string(att))
	expect(t, res, 200, "")
	k := body["key"].(map[string]any)
	if body["ok"] != true || k["name"] != "YubiKey 5C" || k["kind"] != "security-key" || k["id"] == "" || k["createdAt"] == nil {
		t.Fatalf("%v", body)
	}
	if sc := setCookieHeader(res, "ms_session"); !strings.Contains(sc, "HttpOnly") || strings.Contains(sc, "Secure") {
		t.Fatalf("session cookie %q", sc)
	}
	if _, _, pending, _ := b.Auth.Creds.SetupCode(); pending {
		t.Fatal("setup code not consumed")
	}

	// Replaying the finish fails: the ceremony is single use.
	res, _ = c.do(http.MethodPost, "/api/auth/register/finish", string(att))
	expect(t, res, 400, "ceremony_expired")

	_, state := c.do(http.MethodGet, "/api/auth/state", "")
	if state["signedIn"] != true || state["setupNeeded"] != false || state["keys"] != float64(1) {
		t.Fatalf("state %v", state)
	}

	// A fresh browser signs in with the key.
	fresh := newClient(t, b)
	res, _ = fresh.do(http.MethodPost, "/api/auth/login/begin", "")
	expect(t, res, 200, "")
	var login struct {
		PublicKey struct {
			AllowCredentials []struct {
				ID string `json:"id"`
			} `json:"allowCredentials"`
			UserVerification string `json:"userVerification"`
		} `json:"publicKey"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &login)
	if len(login.PublicKey.AllowCredentials) != 1 || login.PublicKey.AllowCredentials[0].ID != k["id"] || login.PublicKey.UserVerification != "preferred" {
		t.Fatalf("login options %s", res.Body.String())
	}
	assertion, err := key.Get(res.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	res, body = fresh.do(http.MethodPost, "/api/auth/login/finish", string(assertion))
	expect(t, res, 200, "")
	if body["ok"] != true || fresh.cookies["ms_session"] == "" {
		t.Fatalf("%v %v", body, fresh.cookies)
	}
	stored, _, _ := b.Auth.Creds.Get(k["id"].(string))
	if stored.Credential.Authenticator.SignCount != 1 || stored.LastUsedAt == nil {
		t.Fatalf("not touched: %+v", stored)
	}

	// An answer signed for another origin fails verification.
	phished := *key
	phished.Origin = "https://evil.example"
	res, _ = fresh.do(http.MethodPost, "/api/auth/login/begin", "")
	assertion, _ = phished.Get(res.Body.Bytes())
	key.Counter = phished.Counter
	res, _ = fresh.do(http.MethodPost, "/api/auth/login/finish", string(assertion))
	expect(t, res, 401, "key_check_failed")

	// A replayed counter looks like a cloned key and is refused.
	clone := *key
	clone.Counter = 0
	res, _ = fresh.do(http.MethodPost, "/api/auth/login/begin", "")
	assertion, _ = clone.Get(res.Body.Bytes())
	res, _ = fresh.do(http.MethodPost, "/api/auth/login/finish", string(assertion))
	expect(t, res, 401, "key_clone_warning")

	// A key that was never added is refused.
	stranger := authtest.New(testOrigin)
	res, _ = fresh.do(http.MethodPost, "/api/auth/login/begin", "")
	assertion, _ = stranger.Get(res.Body.Bytes())
	res, body = fresh.do(http.MethodPost, "/api/auth/login/finish", string(assertion))
	expect(t, res, 401, "key_not_added")
	if e := body["error"].(map[string]any); e["message"] != "That key isn't added to this Mainspring" {
		t.Fatalf("%v", e)
	}

	// Keys under another host are not offered.
	other := newClient(t, b)
	other.host = "127.0.0.1:8317"
	res, _ = other.do(http.MethodPost, "/api/auth/login/begin", "")
	expect(t, res, 409, "no_keys_for_host")

	// Listing, last-key refusal, adding a passkey, rename and delete.
	res, body = fresh.do(http.MethodGet, "/api/auth/keys", "")
	expect(t, res, 200, "")
	keys := body["keys"].([]any)
	first := keys[0].(map[string]any)
	if len(keys) != 1 || first["rpID"] != "localhost" || first["lastUsedAt"] == nil || first["name"] != "YubiKey 5C" {
		t.Fatalf("%v", body)
	}
	res, _ = fresh.do(http.MethodDelete, "/api/auth/keys/"+k["id"].(string), "")
	expect(t, res, 409, "last_key")

	res, _ = fresh.do(http.MethodPost, "/api/auth/register/begin", `{"kind":"passkey","name":"yubikey 5c"}`)
	expect(t, res, 400, "name_taken")
	res, _ = fresh.do(http.MethodPost, "/api/auth/register/begin", `{"kind":"passkey","name":""}`)
	expect(t, res, 400, "name_invalid")
	res, opts = fresh.do(http.MethodPost, "/api/auth/register/begin", `{"kind":"passkey","name":"MacBook"}`)
	expect(t, res, 200, "")
	pk = opts["publicKey"].(map[string]any)
	sel = pk["authenticatorSelection"].(map[string]any)
	if sel["residentKey"] != "required" || sel["requireResidentKey"] != true || sel["authenticatorAttachment"] != nil {
		t.Fatalf("passkey selection %v", sel)
	}
	if ex, _ := pk["excludeCredentials"].([]any); len(ex) != 1 {
		t.Fatalf("exclusions %v", pk["excludeCredentials"])
	}
	passkey := authtest.New(testOrigin)
	att, _ = passkey.Create(res.Body.Bytes())
	res, body = fresh.do(http.MethodPost, "/api/auth/register/finish", string(att))
	expect(t, res, 200, "")
	if setCookieHeader(res, "ms_session") != "" {
		t.Fatal("session reissued outside setup")
	}
	pkID := body["key"].(map[string]any)["id"].(string)

	res, _ = fresh.do(http.MethodPost, "/api/auth/keys/"+pkID+"/rename", `{"name":"YubiKey 5C"}`)
	expect(t, res, 400, "name_taken")
	res, body = fresh.do(http.MethodPost, "/api/auth/keys/"+pkID+"/rename", `{"name":"Laptop"}`)
	expect(t, res, 200, "")
	if body["key"].(map[string]any)["name"] != "Laptop" {
		t.Fatalf("%v", body)
	}
	res, _ = fresh.do(http.MethodDelete, "/api/auth/keys/"+pkID, "")
	expect(t, res, 200, "")
	res, _ = fresh.do(http.MethodDelete, "/api/auth/keys/"+pkID, "")
	expect(t, res, 404, "key_not_found")

	// Logout clears the cookie.
	res, _ = fresh.do(http.MethodPost, "/api/auth/logout", "")
	expect(t, res, 200, "")
	res, _ = fresh.do(http.MethodGet, "/api/auth/keys", "")
	expect(t, res, 401, "panel_signed_out")

	// Old panel routes follow the same gate.
	res, _ = c.do(http.MethodGet, "/api/pool", "")
	expect(t, res, 200, "")
}

func TestProxyMode(t *testing.T) {
	b := bootAuth(t, map[string]string{"MAINSPRING_AUTH": "proxy"})
	c := newClient(t, b)
	c.headers["X-Forwarded-Host"] = "umbrel.local"
	c.headers["X-Forwarded-Proto"] = "https"
	_, state := c.do(http.MethodGet, "/api/auth/state", "")
	if state["mode"] != "proxy" || state["signedIn"] != true || state["setupNeeded"] != false || state["rpID"] != "umbrel.local" {
		t.Fatalf("%v", state)
	}
	res, _ := c.do(http.MethodGet, "/api/auth/keys", "")
	expect(t, res, 200, "")
	res, _ = c.do(http.MethodPut, "/api/auth/require-key", `{"on":true}`)
	expect(t, res, 409, "no_keys_for_host")

	// The proxy vouches for the operator, so a key can be added without a setup code.
	key := authtest.New("https://umbrel.local")
	res, _ = c.do(http.MethodPost, "/api/auth/register/begin", `{"kind":"security-key","name":"YubiKey"}`)
	expect(t, res, 200, "")
	att, _ := key.Create(res.Body.Bytes())
	res, body := c.do(http.MethodPost, "/api/auth/register/finish", string(att))
	expect(t, res, 200, "")
	if sc := setCookieHeader(res, "ms_wa"); !strings.Contains(sc, "Secure") {
		t.Fatalf("ms_wa cookie %q", sc)
	}
	id := body["key"].(map[string]any)["id"].(string)

	res, body = c.do(http.MethodPut, "/api/auth/require-key", `{"on":true}`)
	expect(t, res, 200, "")
	if body["requireKey"] != true {
		t.Fatalf("%v", body)
	}
	_, state = c.do(http.MethodGet, "/api/auth/state", "")
	if state["signedIn"] != false || state["requireKey"] != true {
		t.Fatalf("%v", state)
	}
	res, _ = c.do(http.MethodGet, "/api/auth/keys", "")
	expect(t, res, 401, "panel_signed_out")

	res, _ = c.do(http.MethodPost, "/api/auth/login/begin", "")
	expect(t, res, 200, "")
	assertion, _ := key.Get(res.Body.Bytes())
	res, _ = c.do(http.MethodPost, "/api/auth/login/finish", string(assertion))
	expect(t, res, 200, "")
	if sc := setCookieHeader(res, "ms_session"); !strings.Contains(sc, "Secure") {
		t.Fatalf("session cookie %q", sc)
	}
	res, _ = c.do(http.MethodDelete, "/api/auth/keys/"+id, "")
	expect(t, res, 409, "last_key")

	res, _ = c.do(http.MethodPut, "/api/auth/require-key", `{"on":false}`)
	expect(t, res, 200, "")
	res, _ = c.do(http.MethodDelete, "/api/auth/keys/"+id, "")
	expect(t, res, 200, "")
}

func TestRequireKeyWebAuthnMode(t *testing.T) {
	b := bootAuth(t, nil)
	c := newClient(t, b)
	cookie, _ := b.Auth.IssueSession(httptest.NewRequest(http.MethodGet, "/", nil))
	c.cookies["ms_session"] = strings.TrimPrefix(strings.Split(cookie, ";")[0], "ms_session=")
	res, _ := c.do(http.MethodPut, "/api/auth/require-key", `{"on":true}`)
	expect(t, res, 409, "not_proxy_mode")
}
