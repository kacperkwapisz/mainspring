package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
	"github.com/kacperkwapisz/mainspring/internal/sub"
)

const usageTestProvider domain.ProviderID = "usage-test"

func TestPollUsageReadsOnlyStaleAccountsAndFillsIdentity(t *testing.T) {
	usagePause = 0
	var fetched []string
	sub.Register(usageTestProvider, func(_ context.Context, c domain.Credential) (sub.Snapshot, error) {
		fetched = append(fetched, c.ID)
		used := 40.0
		return sub.Snapshot{Plan: "Max", Email: c.ID + "@example.com", Account: "acct-" + c.ID,
			Windows: []sub.Window{{ID: "five-hour", Label: "5 hours", UsedPercent: &used}}}, nil
	})
	hub, _ := testHub(t, nil)
	save := func(id, label string) {
		if _, err := hub.Vault.Save(store.SaveCredential{ID: id, Provider: usageTestProvider, Label: label, Tokens: domain.CredentialTokens{AccessToken: "t"}}); err != nil {
			t.Fatal(err)
		}
	}
	save("idle", "Claude")    // never observed, default name
	save("named", "Work")     // never observed, a name someone chose
	save("busy", "Claude")    // served a request just now
	save("signedout", "Grok") // needs signing in again
	used := 10.0
	hub.Router.ObserveWindows("busy", "headers", []route.Window{{ID: "x", Label: "5 hours", UsedPercent: &used}})
	hub.Router.MarkSignIn("signedout", "rejected")

	if n := PollUsage(context.Background(), hub, 15*time.Minute); n != 2 {
		t.Fatalf("polled %d accounts (%v), want idle and named", n, fetched)
	}
	idle, _ := hub.Vault.Get("idle")
	if idle.Label != "idle@example.com" || idle.Tokens.Email != "idle@example.com" || idle.Tokens.AccountID != "acct-idle" {
		t.Fatalf("identity not filled: %+v", idle)
	}
	if idle.Tokens.AccessToken != "t" {
		t.Fatal("tokens lost while filling identity")
	}
	if named, _ := hub.Vault.Get("named"); named.Label != "Work" {
		t.Fatalf("a chosen name was replaced: %q", named.Label)
	}
	if st := hub.Router.Get("idle"); st.Source != "usage" || len(st.Windows) == 0 || st.Plan != "Max" {
		t.Fatalf("limits not recorded: %+v", st)
	}
	if n := PollUsage(context.Background(), hub, 15*time.Minute); n != 0 {
		t.Fatalf("fresh accounts polled again: %d", n)
	}
}
