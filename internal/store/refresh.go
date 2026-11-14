package store

import (
	"context"
	"sync"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

// RefreshFunc trades a credential's refresh token for new tokens.
type RefreshFunc func(ctx context.Context, cred domain.Credential) (domain.Credential, error)

// Refresh refreshes a credential and saves the result, one refresh per account
// at a time. Providers such as Claude hand out a new refresh token on every
// refresh and reject the old one, so two callers refreshing with the same token
// would leave one of them with a 400 and the account flagged for sign-in.
//
// Inside the lock it reads the stored credential first. When another caller
// already refreshed it (the stored tokens differ from cred's), it returns the
// stored credential without calling the provider. When the provider refuses,
// it reads the store again and returns the stored credential if its refresh
// token changed meanwhile (a new sign-in landed), so the caller doesn't flag
// an account that is fine.
func (v *Vault) Refresh(ctx context.Context, cred domain.Credential, fn RefreshFunc) (domain.Credential, error) {
	mu, _ := v.refreshLocks.LoadOrStore(cred.ID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	stored, _ := v.Get(cred.ID)
	if stored != nil && tokensChanged(cred, *stored) {
		return *stored, nil
	}
	base := cred
	if stored != nil {
		base = *stored
	}
	out, err := fn(ctx, base)
	if err != nil {
		if again, _ := v.Get(cred.ID); again != nil && again.Tokens.RefreshToken != base.Tokens.RefreshToken {
			return *again, nil
		}
		return base, err
	}
	if stored == nil || !refreshChanged(base, out) {
		return out, nil
	}
	saved, err := v.Save(SaveCredential{
		ID: base.ID, Provider: base.Provider, Label: base.Label, Tokens: out.Tokens, ExpiresAt: out.ExpiresAt,
	})
	if err != nil {
		return out, err
	}
	return saved, nil
}

func tokensChanged(a, b domain.Credential) bool {
	return a.Tokens.AccessToken != b.Tokens.AccessToken || a.Tokens.RefreshToken != b.Tokens.RefreshToken
}

func refreshChanged(before, after domain.Credential) bool {
	if tokensChanged(before, after) {
		return true
	}
	switch {
	case before.ExpiresAt == nil && after.ExpiresAt == nil:
		return false
	case before.ExpiresAt == nil || after.ExpiresAt == nil:
		return true
	default:
		return *before.ExpiresAt != *after.ExpiresAt
	}
}
