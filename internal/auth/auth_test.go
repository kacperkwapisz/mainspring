package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/env"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

const secret = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func newService(t *testing.T, vars map[string]string) *Service {
	t.Helper()
	src := map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": filepath.Join(t.TempDir(), "ms.sqlite"), "MAINSPRING_AUTH": "", "MAINSPRING_TRUST_PROXY": "", "PUBLIC_URL": ""}
	for k, v := range vars {
		src[k] = v
	}
	e, err := env.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(e.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return New(e, store.NewWebAuthnStore(d), store.NewSettingsStore(d))
}

func withCookie(r *http.Request, setCookie string) *http.Request {
	r.Header.Add("Cookie", strings.Split(setCookie, ";")[0])
	return r
}

func TestProxyModeAuthed(t *testing.T) {
	s := newService(t, map[string]string{"MAINSPRING_AUTH": "proxy"})
	if !s.Authed(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("proxy mode should trust the proxy")
	}
	if need, _ := s.SetupNeeded(); need {
		t.Fatal("proxy mode needs no setup")
	}
}

func TestProxyRequireKeyNeedsCookie(t *testing.T) {
	s := newService(t, map[string]string{"MAINSPRING_AUTH": "proxy"})
	if err := s.Settings.Set(SettingRequireKey, "1"); err != nil {
		t.Fatal(err)
	}
	if s.Authed(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("requireKey without cookie should not be authed")
	}
	cookie, err := s.IssueSession(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Authed(withCookie(httptest.NewRequest(http.MethodGet, "/", nil), cookie)) {
		t.Fatal("requireKey with cookie should be authed")
	}
}

func TestWebAuthnModeNeedsCookie(t *testing.T) {
	s := newService(t, nil)
	if s.Authed(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("webauthn mode without cookie should not be authed")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer some-password")
	if s.Authed(r) {
		t.Fatal("bearer password must not sign in")
	}
	forged := httptest.NewRequest(http.MethodGet, "/", nil)
	forged.Header.Set("Cookie", "ms_session=99999999999999.x.y")
	if s.Authed(forged) {
		t.Fatal("forged cookie accepted")
	}
}

func TestEpochRotationInvalidatesCookie(t *testing.T) {
	s := newService(t, nil)
	cookie, err := s.IssueSession(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Authed(withCookie(httptest.NewRequest(http.MethodGet, "/", nil), cookie)) {
		t.Fatal("fresh cookie should be authed")
	}
	if err := s.RotateEpoch(); err != nil {
		t.Fatal(err)
	}
	if s.Authed(withCookie(httptest.NewRequest(http.MethodGet, "/", nil), cookie)) {
		t.Fatal("cookie survived epoch rotation")
	}
}

func TestSetupCookieIsNotASession(t *testing.T) {
	s := newService(t, nil)
	code, err := s.EnsureSetupCode()
	if err != nil || code == "" {
		t.Fatalf("%q %v", code, err)
	}
	setup, err := s.VerifySetupCode(httptest.NewRequest(http.MethodPost, "/", nil), code)
	if err != nil {
		t.Fatal(err)
	}
	r := withCookie(httptest.NewRequest(http.MethodGet, "/", nil), strings.Replace(setup, "ms_setup=", "ms_session=", 1))
	if s.Authed(r) {
		t.Fatal("setup cookie accepted as session")
	}
	if !s.SetupAuthed(withCookie(httptest.NewRequest(http.MethodGet, "/", nil), setup)) {
		t.Fatal("setup cookie rejected")
	}
	// A new code (e.g. a reboot) kills the old setup cookie.
	if _, err := s.NewSetupCode(); err != nil {
		t.Fatal(err)
	}
	if s.SetupAuthed(withCookie(httptest.NewRequest(http.MethodGet, "/", nil), setup)) {
		t.Fatal("setup cookie survived a new code")
	}
}

func TestSetupCodeFormat(t *testing.T) {
	re := regexp.MustCompile(`^[ABCDEFGHJKMNPQRSTUVWXYZ23456789]{4}-[ABCDEFGHJKMNPQRSTUVWXYZ23456789]{4}$`)
	for i := 0; i < 200; i++ {
		if c := GenerateSetupCode(); !re.MatchString(c) {
			t.Fatalf("bad code %q", c)
		}
	}
	if got := NormalizeSetupCode(" k7qm 2xpf "); got != "K7QM-2XPF" {
		t.Fatalf("normalize %q", got)
	}
}

func TestEnsureSetupCodeOnlyWithoutKeys(t *testing.T) {
	s := newService(t, nil)
	if _, err := s.Creds.Add(store.AuthCredential{Name: "k", Kind: KindSecurityKey, RPID: "localhost", Credential: webauthn.Credential{ID: []byte{1}}}); err != nil {
		t.Fatal(err)
	}
	if code, err := s.EnsureSetupCode(); err != nil || code != "" {
		t.Fatalf("%q %v", code, err)
	}
}

func TestRelyingParty(t *testing.T) {
	s := newService(t, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "LocalHost:8317"
	r.Header.Set("X-Forwarded-Host", "evil.example")
	r.Header.Set("X-Forwarded-Proto", "https")
	rp := s.RelyingParty(r)
	if rp.ID != "localhost" || rp.Origin != "http://localhost:8317" || rp.Secure || !rp.SecureContext {
		t.Fatalf("%+v", rp)
	}

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "hub.lan:8317"
	if rp := s.RelyingParty(r); rp.SecureContext {
		t.Fatalf("plain http LAN host should not be secure: %+v", rp)
	}
	r.TLS = &tls.ConnectionState{}
	if rp := s.RelyingParty(r); rp.Origin != "https://hub.lan:8317" || !rp.SecureContext || !rp.Secure {
		t.Fatalf("%+v", rp)
	}

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	// Browsers refuse IP addresses as a relying party, loopback included.
	for _, host := range []string{"[::1]:8317", "127.0.0.1:8317", "192.168.1.20:8317"} {
		r.Host = host
		if rp := s.RelyingParty(r); rp.SecureContext {
			t.Fatalf("%s must not offer security keys: %+v", host, rp)
		}
	}

	proxy := newService(t, map[string]string{"MAINSPRING_AUTH": "proxy"})
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "10.0.0.2:8317"
	r.Header.Set("X-Forwarded-Host", "umbrel.example, inner")
	r.Header.Set("X-Forwarded-Proto", "https")
	if rp := proxy.RelyingParty(r); rp.ID != "umbrel.example" || rp.Origin != "https://umbrel.example" || !rp.SecureContext {
		t.Fatalf("%+v", rp)
	}

	trusted := newService(t, map[string]string{"MAINSPRING_TRUST_PROXY": "1", "PUBLIC_URL": "https://hub.example"})
	if rp := trusted.RelyingParty(r); rp.ID != "umbrel.example" {
		t.Fatalf("%+v", rp)
	}
	if trusted.HTTPSURL() != "https://hub.example" || s.HTTPSURL() != "" {
		t.Fatal("httpsURL")
	}
}
