package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/domain"
)

func refreshVault(t *testing.T) *Vault {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return NewVault(d, make([]byte, 32))
}

var errRejected = errors.New("refresh token rejected")

// rotating behaves like Claude's token endpoint: every refresh returns a new
// refresh token and the old one stops working.
type rotating struct {
	mu    sync.Mutex
	valid string
	calls atomic.Int32
}

func (r *rotating) refresh(_ context.Context, c domain.Credential) (domain.Credential, error) {
	r.calls.Add(1)
	time.Sleep(10 * time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	if c.Tokens.RefreshToken != r.valid {
		return c, errRejected
	}
	n := r.calls.Load()
	r.valid = "r" + string(rune('0'+n))
	c.Tokens.AccessToken = "a" + string(rune('0'+n))
	c.Tokens.RefreshToken = r.valid
	exp := time.Now().Add(8 * time.Hour).UnixMilli()
	c.ExpiresAt = &exp
	return c, nil
}

func TestConcurrentRefreshesUseTheTokenOnce(t *testing.T) {
	v := refreshVault(t)
	exp := time.Now().Add(time.Minute).UnixMilli()
	cred, err := v.Save(SaveCredential{ID: "c", Provider: domain.ProviderClaude, Label: "Claude",
		Tokens: domain.CredentialTokens{AccessToken: "a0", RefreshToken: "r0"}, ExpiresAt: &exp})
	if err != nil {
		t.Fatal(err)
	}
	ep := &rotating{valid: "r0"}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := v.Refresh(context.Background(), cred, ep.refresh)
			if err != nil {
				errs <- err
				return
			}
			if got.Tokens.RefreshToken != ep.valid {
				errs <- errors.New("caller got a stale refresh token " + got.Tokens.RefreshToken)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := ep.calls.Load(); n != 1 {
		t.Fatalf("token endpoint called %d times, want 1", n)
	}
	stored, _ := v.Get("c")
	if stored.Tokens.RefreshToken != ep.valid || stored.Tokens.AccessToken == "a0" {
		t.Fatalf("stored %+v", stored.Tokens)
	}
}

func TestRefreshWithStaleCopyUsesStoredToken(t *testing.T) {
	v := refreshVault(t)
	stale, _ := v.Save(SaveCredential{ID: "c", Provider: domain.ProviderClaude, Label: "Claude",
		Tokens: domain.CredentialTokens{AccessToken: "a0", RefreshToken: "r0"}})
	ep := &rotating{valid: "r0"}
	if _, err := v.Refresh(context.Background(), stale, ep.refresh); err != nil {
		t.Fatal(err)
	}
	// A request that read the account before that refresh gets a 401 and
	// refreshes with its old copy: it must get the stored tokens, not a 400.
	got, err := v.Refresh(context.Background(), stale, ep.refresh)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tokens.RefreshToken != ep.valid || ep.calls.Load() != 1 {
		t.Fatalf("got %+v after %d calls", got.Tokens, ep.calls.Load())
	}
}

func TestRejectedRefreshReturnsNewSignIn(t *testing.T) {
	v := refreshVault(t)
	cred, _ := v.Save(SaveCredential{ID: "c", Provider: domain.ProviderClaude, Label: "Claude",
		Tokens: domain.CredentialTokens{AccessToken: "a0", RefreshToken: "dead"}})
	fn := func(_ context.Context, c domain.Credential) (domain.Credential, error) {
		// A new sign-in saves fresh tokens while this refresh is in flight.
		_, _ = v.Save(SaveCredential{ID: "c", Provider: domain.ProviderClaude, Label: "Claude",
			Tokens: domain.CredentialTokens{AccessToken: "fresh", RefreshToken: "fresh-r"}})
		return c, errRejected
	}
	got, err := v.Refresh(context.Background(), cred, fn)
	if err != nil || got.Tokens.AccessToken != "fresh" {
		t.Fatalf("got %+v err %v", got.Tokens, err)
	}
	cred2, _ := v.Get("c")
	if _, err := v.Refresh(context.Background(), *cred2, func(_ context.Context, c domain.Credential) (domain.Credential, error) {
		return c, errRejected
	}); !errors.Is(err, errRejected) {
		t.Fatalf("a real rejection must surface, got %v", err)
	}
}

func TestRefreshKeepsRenameAndDeletion(t *testing.T) {
	v := refreshVault(t)
	cred, _ := v.Save(SaveCredential{ID: "c", Provider: domain.ProviderClaude, Label: "Claude",
		Tokens: domain.CredentialTokens{AccessToken: "a0", RefreshToken: "r0"}})
	_ = v.Rename("c", "Work")
	ep := &rotating{valid: "r0"}
	got, err := v.Refresh(context.Background(), cred, ep.refresh)
	if err != nil || got.Label != "Work" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := v.db.SQL.Exec(`DELETE FROM credentials WHERE id = 'c'`); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Refresh(context.Background(), got, ep.refresh); err != nil {
		t.Fatal(err)
	}
	if c, _ := v.Get("c"); c != nil {
		t.Fatal("refresh brought a deleted account back")
	}
}
