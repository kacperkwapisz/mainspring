package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/kacperkwapisz/mainspring/internal/auth"
	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/env"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

const secret = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestAuthResetAndStatus(t *testing.T) {
	dir := t.TempDir()
	src := map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": filepath.Join(dir, "ms.sqlite"), "MAINSPRING_HOME": dir, "MAINSPRING_AUTH": ""}

	// Seed a key and a live session the way the hub would.
	e, err := env.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(e.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	svc := auth.New(e, store.NewWebAuthnStore(d), store.NewSettingsStore(d))
	if _, err := svc.Creds.Add(store.AuthCredential{Name: "YubiKey", Kind: auth.KindSecurityKey, RPID: "localhost", Credential: webauthn.Credential{ID: []byte{1, 2}}}); err != nil {
		t.Fatal(err)
	}
	cookie, err := svc.IssueSession(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	signedIn := func() bool {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Cookie", strings.Split(cookie, ";")[0])
		return svc.Authed(r)
	}
	if !signedIn() {
		t.Fatal("session should work before reset")
	}

	var out, errOut bytes.Buffer
	if code := runAuth([]string{"status"}, src, &out, &errOut); code != 0 {
		t.Fatalf("status exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "mode: webauthn") || !strings.Contains(out.String(), "keys: 1") || !strings.Contains(out.String(), "YubiKey (security-key, localhost)") {
		t.Fatalf("status output %q", out.String())
	}

	out.Reset()
	if code := runAuth([]string{"reset"}, src, &out, &errOut); code != 0 {
		t.Fatalf("reset exit %d: %s", code, errOut.String())
	}
	m := regexp.MustCompile(`mainspring setup code: ([A-Z2-9]{4}-[A-Z2-9]{4}) — open the panel to add your first security key`).FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("reset output %q", out.String())
	}
	if n, _ := svc.Creds.Count(); n != 0 {
		t.Fatalf("keys left: %d", n)
	}
	if signedIn() {
		t.Fatal("session survived reset")
	}
	if ok, _ := svc.Creds.MatchSetupCode(m[1]); !ok {
		t.Fatal("printed setup code does not match the stored one")
	}
	d.Close()

	if code := runAuth([]string{"nope"}, src, &out, &errOut); code != 2 {
		t.Fatalf("bad subcommand exit %d", code)
	}
}
