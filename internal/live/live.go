// Package live tells open panels what changed, so they reload only that.
//
// Stores and the router publish topics ("pool", "sessions", "meter", "keys",
// "settings") when their data changes. Each subscriber collects the topics it
// hasn't been sent yet; bursts (a busy proxy records usage on every request)
// collapse into one set per wake-up.
package live

import "sync"

// Topics the panel understands.
const (
	Pool     = "pool"
	Sessions = "sessions"
	Meter    = "meter"
	Keys     = "keys"
	Settings = "settings"
	Updates  = "updates"
	Log      = "log" // a line was logged; the panel's log viewer reads /api/log
)

// Bus fans change notifications out to subscribers. A nil *Bus is valid and
// drops everything, so code paths without a panel (tests, the CLI) need no setup.
type Bus struct {
	mu   sync.Mutex
	subs map[*Sub]struct{}
}

func New() *Bus { return &Bus{subs: map[*Sub]struct{}{}} }

// Sub is one listener. Wake fires when there is something to Take.
type Sub struct {
	mu      sync.Mutex
	pending map[string]bool
	Wake    chan struct{}
}

// Publish marks topics as changed for every subscriber. It never blocks.
func (b *Bus) Publish(topics ...string) {
	if b == nil || len(topics) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		s.mu.Lock()
		for _, t := range topics {
			s.pending[t] = true
		}
		s.mu.Unlock()
		select {
		case s.Wake <- struct{}{}:
		default:
		}
	}
}

// Subscribe registers a listener; call the returned func to drop it.
func (b *Bus) Subscribe() (*Sub, func()) {
	s := &Sub{pending: map[string]bool{}, Wake: make(chan struct{}, 1)}
	if b == nil {
		return s, func() {}
	}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s, func() {
		b.mu.Lock()
		delete(b.subs, s)
		b.mu.Unlock()
	}
}

// Take returns the topics changed since the last Take and clears them.
func (s *Sub) Take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.pending))
	for t := range s.pending {
		out = append(out, t)
	}
	clear(s.pending)
	return out
}
