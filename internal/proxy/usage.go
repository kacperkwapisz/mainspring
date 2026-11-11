package proxy

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/sub"
)

// Limits come from two places. Response headers are exact and free, but only
// arrive when an account serves a request. The providers' usage endpoints (the
// ones Claude Code's /usage and Codex's /status read) cover the rest: accounts
// that just signed in, and accounts that have been idle long enough to go stale.

// UsageMaxAge is how old an account's limits may get before Mainspring reads
// them from the usage endpoint. Busy accounts never need it: their limits
// arrive with every response.
const UsageMaxAge = 30 * time.Minute

// usageBackoff holds, per account, when its usage endpoint may be read again
// after it answered 429.
var usageBackoff sync.Map // credential id -> unix ms

// usagePause spaces out background reads so a large pool doesn't burst.
var usagePause = 2 * time.Second

// CheckUsage reads one account's limits from its usage endpoint and records
// them. It also fills in the account's email when the sign-in didn't report one.
func CheckUsage(ctx context.Context, hub *Hub, cred domain.Credential) sub.Snapshot {
	if until, ok := usageBackoff.Load(cred.ID); ok && until.(int64) > time.Now().UnixMilli() {
		return sub.Snapshot{ID: cred.ID, Provider: cred.Provider, Label: cred.Label, Error: "rate limited", RetryAt: until.(int64)}
	}
	snap := sub.Fetch(ctx, hub.Vault, hub.Executors, cred)
	if !snap.OK {
		if snap.RetryAt > 0 {
			usageBackoff.Store(cred.ID, snap.RetryAt)
		}
		return snap
	}
	usageBackoff.Delete(cred.ID)
	if ws := UsageWindows(cred.Provider, snap.Windows); len(ws) > 0 {
		hub.Router.ObserveWindows(cred.ID, "usage", ws)
	}
	if snap.Plan != "" {
		hub.Router.SetPlan(cred.ID, snap.Plan)
	}
	fillIdentity(hub, cred.ID, snap)
	return snap
}

// PollUsage reads limits for every account whose limits are older than maxAge.
// Accounts that need signing in again, or whose token is about to expire, are
// left alone: the keepalive refreshes tokens, and a request path owns 401s.
func PollUsage(ctx context.Context, hub *Hub, maxAge time.Duration) int {
	if hub == nil || hub.Vault == nil || hub.Router == nil {
		return 0
	}
	creds, err := hub.Vault.List()
	if err != nil {
		return 0
	}
	n := 0
	for _, cred := range creds {
		if !usageDue(hub, cred, maxAge) {
			continue
		}
		if n > 0 {
			select {
			case <-ctx.Done():
				return n
			case <-time.After(usagePause):
			}
		}
		c, cancel := context.WithTimeout(ctx, sub.CredentialTimeout)
		CheckUsage(c, hub, cred)
		cancel()
		n++
	}
	return n
}

func usageDue(hub *Hub, cred domain.Credential, maxAge time.Duration) bool {
	if !sub.Supported(cred.Provider) || nearExpiry(cred) {
		return false
	}
	st := hub.Router.Get(cred.ID)
	if st.NeedsSignIn {
		return false
	}
	if until, ok := usageBackoff.Load(cred.ID); ok && until.(int64) > time.Now().UnixMilli() {
		return false
	}
	return st.ObservedAt == 0 || time.Now().UnixMilli()-st.ObservedAt > maxAge.Milliseconds()
}

// defaultLabels are the names a sign-in falls back to when it doesn't say whose
// account it is. Only these are replaced by the email; a name you chose stays.
var defaultLabels = map[string]bool{"Claude": true, "Codex": true, "Grok": true, "Cursor": true, "Cursor key": true}

func fillIdentity(hub *Hub, id string, snap sub.Snapshot) {
	email := strings.TrimSpace(snap.Email)
	if email == "" && snap.Account == "" {
		return
	}
	_, _ = hub.Vault.FillIdentity(id, email, snap.Account, func(label string) bool { return defaultLabels[label] })
}

// UsageWindows converts usage-endpoint windows to router windows. The main
// windows take the header ids so both sources describe the same window; every
// other window keeps its own id.
func UsageWindows(p domain.ProviderID, in []sub.Window) []route.Window {
	out := make([]route.Window, 0, len(in))
	for _, w := range in {
		rw := route.Window{ID: string(p) + "-usage-" + w.ID, Label: w.Label, UsedPercent: w.UsedPercent, ResetsAt: w.ResetsAt}
		switch {
		case strings.Contains(w.ID, "five-hour"):
			rw.PeriodMS = 5 * 60 * 60 * 1000
		case strings.Contains(w.ID, "seven-day") || strings.Contains(w.ID, "weekly"):
			rw.PeriodMS = 7 * 24 * 60 * 60 * 1000
		case strings.Contains(w.ID, "monthly"):
			rw.PeriodMS = 30 * 24 * 60 * 60 * 1000
		}
		switch {
		case p == domain.ProviderClaude && w.ID == "five-hour":
			rw.ID = "claude-5h"
		case p == domain.ProviderClaude && w.ID == "seven-day":
			rw.ID = "claude-7d"
		case p == domain.ProviderCodex && w.ID == "five-hour":
			rw.ID = "codex-primary"
		case p == domain.ProviderCodex && (w.ID == "weekly" || w.ID == "monthly"):
			rw.ID = "codex-secondary"
		}
		if rw.UsedPercent != nil && *rw.UsedPercent >= 100 {
			rw.Rejected = true
		}
		if strings.HasPrefix(w.ID, "code-review") {
			rw.Soft = true // a separate code-review allowance never blocks coding traffic
		}
		out = append(out, rw)
	}
	return out
}
