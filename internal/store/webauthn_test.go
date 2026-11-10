package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/kacperkwapisz/mainspring/internal/db"
)

func openTemp(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "ms.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestWebAuthnCredentials(t *testing.T) {
	s := NewWebAuthnStore(openTemp(t))
	a, err := s.Add(AuthCredential{Name: "YubiKey", Kind: "security-key", RPID: "localhost",
		Credential: webauthn.Credential{ID: []byte{1, 2, 3}, PublicKey: []byte{9}, Authenticator: webauthn.Authenticator{SignCount: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "AQID" || a.CreatedAt == 0 {
		t.Fatalf("%+v", a)
	}
	if _, err := s.Add(AuthCredential{Name: "Other", Kind: "passkey", RPID: "localhost", Credential: webauthn.Credential{ID: []byte{1, 2, 3}}}); err != ErrCredentialExists {
		t.Fatalf("dup: %v", err)
	}
	if _, err := s.Add(AuthCredential{Name: "Phone", Kind: "passkey", RPID: "hub.example", Credential: webauthn.Credential{ID: []byte{7}}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(); n != 2 {
		t.Fatalf("count %d", n)
	}
	local, _ := s.ListForRP("localhost")
	if len(local) != 1 || local[0].Credential.Authenticator.SignCount != 4 || local[0].LastUsedAt != nil {
		t.Fatalf("%+v", local)
	}
	if taken, _ := s.NameTaken("yubikey", ""); !taken {
		t.Fatal("name should be taken")
	}
	if taken, _ := s.NameTaken("YubiKey", a.ID); taken {
		t.Fatal("own name should not count")
	}
	if ok, _ := s.Rename(a.ID, "Blue key"); !ok {
		t.Fatal("rename")
	}
	cred := local[0].Credential
	cred.Authenticator.SignCount = 9
	if err := s.Touch(a.ID, cred, 1234); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := s.Get(a.ID)
	if !ok || got.Name != "Blue key" || got.Credential.Authenticator.SignCount != 9 || got.LastUsedAt == nil || *got.LastUsedAt != 1234 {
		t.Fatalf("%+v", got)
	}
	if ok, _ := s.Delete(a.ID); !ok {
		t.Fatal("delete")
	}
	if ok, _ := s.Delete(a.ID); ok {
		t.Fatal("delete twice")
	}
	if n, _ := s.DeleteAll(); n != 1 {
		t.Fatalf("delete all %d", n)
	}
}

func TestSetupCodeHashOnlyAndConsumed(t *testing.T) {
	d := openTemp(t)
	s := NewWebAuthnStore(d)
	if _, _, ok, _ := s.SetupCode(); ok {
		t.Fatal("no code yet")
	}
	if _, err := s.PutSetupCode("K7QM-2XPF"); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := d.SQL.QueryRow(`SELECT code_hash FROM auth_setup WHERE id = 1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "K7QM") || len(stored) != 64 {
		t.Fatalf("stored %q", stored)
	}
	if ok, _ := s.MatchSetupCode("K7QM-2XPG"); ok {
		t.Fatal("wrong code matched")
	}
	if ok, _ := s.MatchSetupCode("K7QM-2XPF"); !ok {
		t.Fatal("right code did not match")
	}
	if err := s.ConsumeSetupCode(); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.MatchSetupCode("K7QM-2XPF"); ok {
		t.Fatal("consumed code matched")
	}
	if _, _, ok, _ := s.SetupCode(); ok {
		t.Fatal("code should be gone")
	}
}
