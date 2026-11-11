package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/live"
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

func TestWatchResetsAnnouncesTheResetAndReadsTheNewWindow(t *testing.T) {
	usagePause, resetSettle, resetRecheck = 0, 50*time.Millisecond, 10*time.Millisecond
	const p domain.ProviderID = "reset-test"
	read := make(chan string, 4)
	sub.Register(p, func(_ context.Context, c domain.Credential) (sub.Snapshot, error) {
		read <- c.ID
		used := 3.0
		return sub.Snapshot{Windows: []sub.Window{{ID: "five-hour", Label: "5 hours", UsedPercent: &used}}}, nil
	})
	hub, _ := testHub(t, nil)
	hub.Live = live.New()
	hub.Router.Live = hub.Live
	if _, err := hub.Vault.Save(store.SaveCredential{ID: "a", Provider: p, Label: "a@example.com", Tokens: domain.CredentialTokens{AccessToken: "t"}}); err != nil {
		t.Fatal(err)
	}
	used, reset := 100.0, time.Now().Add(300*time.Millisecond).UnixMilli()
	hub.Router.ObserveWindows("a", "headers", []route.Window{{ID: "w", Label: "5 hours", UsedPercent: &used, ResetsAt: &reset}})

	panel, drop := hub.Live.Subscribe()
	defer drop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go WatchResets(ctx, hub)

	deadline := time.After(3 * time.Second)
	for announced := false; !announced; {
		select {
		case <-panel.Wake:
			for _, topic := range panel.Take() {
				announced = announced || (topic == live.Pool && time.Now().UnixMilli() >= reset)
			}
		case <-deadline:
			t.Fatal("the reset was never announced")
		}
	}
	select {
	case id := <-read:
		if id != "a" {
			t.Fatalf("read %s", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("limits weren't read after the reset")
	}
}

func TestUsage429BacksOffInsteadOfRetryingEveryPoll(t *testing.T) {
	usagePause = 0
	const p domain.ProviderID = "ratelimit-test"
	calls := 0
	sub.Register(p, func(context.Context, domain.Credential) (sub.Snapshot, error) {
		calls++
		return sub.Snapshot{}, &sub.RateLimited{Provider: "Anthropic"}
	})
	hub, _ := testHub(t, nil)
	c, _ := hub.Vault.Save(store.SaveCredential{Provider: p, Label: "pat@example.com", Tokens: domain.CredentialTokens{AccessToken: "t"}})
	if snap := CheckUsage(context.Background(), hub, c); snap.OK || snap.RetryAt < time.Now().Add(29*time.Minute).UnixMilli() {
		t.Fatalf("%+v", snap)
	}
	PollUsage(context.Background(), hub, 0)
	if snap := CheckUsage(context.Background(), hub, c); snap.RetryAt == 0 || calls != 1 {
		t.Fatalf("the endpoint was asked again during the back-off: %d calls", calls)
	}
}
