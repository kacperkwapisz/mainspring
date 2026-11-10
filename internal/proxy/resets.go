package proxy

import (
	"context"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/live"
)

// When a quota window resets or a cooldown ends, nothing arrives from the
// provider: the account simply becomes usable again. WatchResets turns those
// moments into events, so open panels update on time, and then reads the
// account's limits so the new window's reset time is known.

var (
	// resetSettle gives the provider a moment to start the new window before
	// Mainspring reads it.
	resetSettle = 30 * time.Second
	// resetRecheck bounds how often a busy pool makes the watcher recompute.
	resetRecheck = 2 * time.Second
)

// WatchResets runs until ctx ends.
func WatchResets(ctx context.Context, hub *Hub) {
	if hub == nil || hub.Router == nil || hub.Vault == nil {
		return
	}
	sub, drop := hub.Live.Subscribe()
	defer drop()
	last := time.Now().UnixMilli()
	for {
		now := time.Now().UnixMilli()
		wait := time.Hour
		if next := nextReset(hub, now); next > 0 {
			wait = time.Duration(next-now)*time.Millisecond + 250*time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-sub.Wake:
			// Something changed (new limits, a new cooldown): recompute, but not
			// on every request of a busy pool.
			timer.Stop()
			sub.Take()
			select {
			case <-ctx.Done():
				return
			case <-time.After(resetRecheck):
			}
		case <-timer.C:
			now := time.Now().UnixMilli()
			due := resetBetween(hub, last, now)
			last = now
			if len(due) == 0 {
				continue
			}
			hub.Live.Publish(live.Pool, live.Sessions)
			go readAfterReset(ctx, hub, due, now)
		}
	}
}

// nextReset is the soonest future moment an account's window resets or its
// cooldown ends, or 0 when nothing is pending.
func nextReset(hub *Hub, now int64) int64 {
	creds, err := hub.Vault.List()
	if err != nil {
		return 0
	}
	var next int64
	soonest := func(t int64) {
		if t > now && (next == 0 || t < next) {
			next = t
		}
	}
	for _, c := range creds {
		st := hub.Router.Get(c.ID)
		soonest(st.CoolUntil)
		for _, w := range st.Windows {
			if w.ResetsAt != nil {
				soonest(*w.ResetsAt)
			}
		}
	}
	return next
}

// resetBetween lists accounts with a window reset or cooldown end in (from, to].
func resetBetween(hub *Hub, from, to int64) []domain.Credential {
	creds, err := hub.Vault.List()
	if err != nil {
		return nil
	}
	in := func(t int64) bool { return t > from && t <= to }
	var out []domain.Credential
	for _, c := range creds {
		st := hub.Router.Get(c.ID)
		hit := in(st.CoolUntil)
		for _, w := range st.Windows {
			if w.ResetsAt != nil && in(*w.ResetsAt) {
				hit = true
			}
		}
		if hit {
			out = append(out, c)
		}
	}
	return out
}

// readAfterReset reads the limits of accounts whose window just reset, unless
// a request has already brought fresh limits since.
func readAfterReset(ctx context.Context, hub *Hub, creds []domain.Credential, resetAt int64) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(resetSettle):
	}
	n := 0
	for _, c := range creds {
		if !usageDue(hub, c, 0) || hub.Router.Get(c.ID).ObservedAt > resetAt {
			continue
		}
		if n > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(usagePause):
			}
		}
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		CheckUsage(cctx, hub, c)
		cancel()
		n++
	}
}
