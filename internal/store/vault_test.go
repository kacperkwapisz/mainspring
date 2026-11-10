package store

import (
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/domain"
)

func TestSaveLoginUpdatesSameAccount(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	v := NewVault(d, make([]byte, 32))
	first, existed, err := v.SaveLogin(SaveCredential{Provider: domain.ProviderClaude, Label: "kacper@x.pl", Tokens: domain.CredentialTokens{AccessToken: "1", Email: "kacper@x.pl"}})
	if err != nil || existed {
		t.Fatal(err, existed)
	}
	_ = v.Rename(first.ID, "Main")
	again, existed, err := v.SaveLogin(SaveCredential{Provider: domain.ProviderClaude, Label: "kacper@x.pl", Tokens: domain.CredentialTokens{AccessToken: "2", Email: "KACPER@x.pl"}})
	if err != nil || !existed || again.ID != first.ID || again.Label != "Main" {
		t.Fatalf("%+v %v %v", again, existed, err)
	}
	other, existed, _ := v.SaveLogin(SaveCredential{Provider: domain.ProviderClaude, Label: "work@x.pl", Tokens: domain.CredentialTokens{AccessToken: "3", Email: "work@x.pl"}})
	if existed || other.ID == first.ID {
		t.Fatal("different account must be new")
	}
	list, _ := v.List(domain.ProviderClaude)
	if len(list) != 2 || list[0].Tokens.AccessToken != "2" {
		t.Fatalf("%+v", list)
	}
}
