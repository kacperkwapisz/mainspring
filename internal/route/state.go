package route

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/domain"
)

// State is what the router knows about one credential besides its tokens.
type State struct {
	Windows     []Window `json:"windows,omitempty"`
	ObservedAt  int64    `json:"observed_at,omitempty"` // when Windows were last read
	Source      string   `json:"source,omitempty"`      // "headers" or "usage"
	CoolUntil   int64    `json:"cool_until,omitempty"`
	CoolWindow  string   `json:"cool_window,omitempty"` // label of the window that ran out
	CoolSince   int64    `json:"cool_since,omitempty"`
	Failures    int      `json:"failures,omitempty"` // consecutive 429s without a reset time
	NeedsSignIn bool     `json:"needs_sign_in,omitempty"`
	SignInError string   `json:"sign_in_error,omitempty"`
	SignInAt    int64    `json:"sign_in_at,omitempty"`
	Benched     bool     `json:"benched,omitempty"`
	BenchedAt   int64    `json:"benched_at,omitempty"`
	Plan        string   `json:"plan,omitempty"`
}

// Cooling reports whether the credential rests at time now.
func (s State) Cooling(now int64) bool { return s.CoolUntil > now }

// Usable reports whether new requests may go to this credential at time now.
func (s State) Usable(now int64) bool {
	return !s.Benched && !s.NeedsSignIn && !s.Cooling(now) && s.spentUntil(now) == 0
}

// spentUntil returns when the latest spent window resets, or 0 when none is spent.
func (s State) spentUntil(now int64) int64 {
	var until int64
	for _, w := range s.Windows {
		if w.Spent(now) && w.ResetsAt != nil && *w.ResetsAt > until {
			until = *w.ResetsAt
		}
	}
	return until
}

// BackAt returns when an unusable credential is expected to serve again (0 = unknown).
func (s State) BackAt(now int64) int64 {
	if s.Benched || s.NeedsSignIn {
		return 0
	}
	back := s.CoolUntil
	if sp := s.spentUntil(now); sp > back {
		back = sp
	}
	if back <= now {
		return 0
	}
	return back
}

// LongWindow returns the window with the longest horizon that can block the
// account. Soonest-reset-first routing compares accounts by this window.
func (s State) LongWindow(now int64) (Window, bool) {
	var best Window
	found := false
	for _, w := range s.Windows {
		if w.Soft || w.ResetsAt == nil || *w.ResetsAt <= now {
			continue
		}
		if !found || w.horizon(now) > best.horizon(now) {
			best, found = w, true
		}
	}
	return best, found
}

// ShortWindow returns the blocking window with the shortest horizon.
func (s State) ShortWindow(now int64) (Window, bool) {
	var best Window
	found := false
	for _, w := range s.Windows {
		if w.Soft {
			continue
		}
		if !found || w.horizon(now) < best.horizon(now) {
			best, found = w, true
		}
	}
	return best, found
}

// Engine holds routing state for every credential, session bindings and live sessions.
type Engine struct {
	mu       sync.Mutex
	db       *db.DB
	states   map[string]*State
	bindings map[string]binding
	sessions map[string]*Session
	ttl      time.Duration
	now      func() int64
	lastSave map[string]int64
}

type binding struct {
	credID string
	until  int64
}

const createStateTable = `CREATE TABLE IF NOT EXISTS credential_state (
  id TEXT PRIMARY KEY,
  state TEXT NOT NULL,
  updated_at INTEGER NOT NULL
)`

// New loads persisted state. d may be nil in tests.
func New(d *db.DB) (*Engine, error) {
	e := &Engine{
		db: d, states: map[string]*State{}, bindings: map[string]binding{},
		sessions: map[string]*Session{}, ttl: time.Hour, lastSave: map[string]int64{},
		now: func() int64 { return time.Now().UnixMilli() },
	}
	if d == nil {
		return e, nil
	}
	if _, err := d.SQL.Exec(createStateTable); err != nil {
		return nil, err
	}
	rows, err := d.SQL.Query(`SELECT id, state FROM credential_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var s State
		if json.Unmarshal([]byte(raw), &s) == nil {
			e.states[id] = &s
		}
	}
	return e, rows.Err()
}

// SetAffinityTTL sets how long a session stays on its account after its last request.
func (e *Engine) SetAffinityTTL(d time.Duration) {
	if d <= 0 {
		return
	}
	e.mu.Lock()
	e.ttl = d
	e.mu.Unlock()
}

// AffinityTTL returns the current session affinity window.
func (e *Engine) AffinityTTL() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ttl
}

// Get returns a copy of the state for a credential.
func (e *Engine) Get(id string) State {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s := e.states[id]; s != nil {
		cp := *s
		cp.Windows = append([]Window(nil), s.Windows...)
		return cp
	}
	return State{}
}

func (e *Engine) state(id string) *State {
	s := e.states[id]
	if s == nil {
		s = &State{}
		e.states[id] = s
	}
	return s
}

// persist writes the state; force skips the write throttle.
func (e *Engine) persist(id string, force bool) {
	if e.db == nil {
		return
	}
	now := e.now()
	if !force && now-e.lastSave[id] < 15_000 {
		return
	}
	e.lastSave[id] = now
	raw, _ := json.Marshal(e.states[id])
	_, _ = e.db.SQL.Exec(`INSERT INTO credential_state(id, state, updated_at) VALUES (?, ?, ?)
	  ON CONFLICT(id) DO UPDATE SET state = excluded.state, updated_at = excluded.updated_at`, id, string(raw), now)
}

// Forget drops state for a removed credential.
func (e *Engine) Forget(id string) {
	e.mu.Lock()
	delete(e.states, id)
	for k, b := range e.bindings {
		if b.credID == id {
			delete(e.bindings, k)
		}
	}
	e.mu.Unlock()
	if e.db != nil {
		_, _ = e.db.SQL.Exec(`DELETE FROM credential_state WHERE id = ?`, id)
	}
}

// SetBenched takes a credential out of rotation or puts it back.
func (e *Engine) SetBenched(id string, benched bool) {
	e.mu.Lock()
	s := e.state(id)
	s.Benched = benched
	if benched {
		s.BenchedAt = e.now()
	} else {
		s.BenchedAt = 0
	}
	e.persist(id, true)
	e.mu.Unlock()
}

// SignedIn clears sign-in and cooldown problems after a fresh login for id.
func (e *Engine) SignedIn(id string) {
	e.mu.Lock()
	s := e.state(id)
	s.NeedsSignIn, s.SignInError, s.SignInAt = false, "", 0
	s.CoolUntil, s.CoolWindow, s.CoolSince, s.Failures = 0, "", 0, 0
	e.persist(id, true)
	e.mu.Unlock()
}

// MarkSignIn records that the provider rejected the credential's refresh token.
func (e *Engine) MarkSignIn(id, reason string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.state(id)
	if s.NeedsSignIn {
		return false
	}
	s.NeedsSignIn, s.SignInError, s.SignInAt = true, reason, e.now()
	e.persist(id, true)
	return true
}

// ObserveWindows stores windows read from response headers or the usage API.
// Windows with the same ID are replaced; windows from the same source that are
// missing from the new set are dropped; windows from the other source are kept
// until they reset.
func (e *Engine) ObserveWindows(id, source string, ws []Window) {
	if len(ws) == 0 {
		return
	}
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.state(id)
	incoming := map[string]bool{}
	for _, w := range ws {
		incoming[w.ID] = true
	}
	merged := append([]Window(nil), ws...)
	for _, old := range s.Windows {
		if incoming[old.ID] || s.Source == source {
			continue
		}
		if old.ResetsAt != nil && *old.ResetsAt <= now {
			continue
		}
		merged = append(merged, old)
	}
	SortWindows(merged, now)
	s.Windows, s.ObservedAt, s.Source = merged, now, source
	// A window that has headroom again ends a cooldown caused by running out.
	if s.CoolUntil > now && s.CoolWindow != "" && s.spentUntil(now) == 0 {
		for _, w := range ws {
			if w.Label == s.CoolWindow && !w.Spent(now) && w.UsedPercent != nil {
				s.CoolUntil, s.CoolWindow, s.CoolSince = 0, "", 0
				break
			}
		}
	}
	e.persist(id, false)
}

// Succeeded records a good response.
func (e *Engine) Succeeded(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.state(id)
	if s.Failures == 0 && s.CoolUntil == 0 {
		return
	}
	s.Failures = 0
	if s.CoolWindow == "" {
		s.CoolUntil, s.CoolSince = 0, 0
	}
	e.persist(id, true)
}

// Cooldown backoff for 429s without a usable reset time.
func backoff(failures int) int64 {
	d := int64(30_000)
	for i := 1; i < failures && d < 30*60*1000; i++ {
		d *= 2
	}
	if d > 30*60*1000 {
		d = 30 * 60 * 1000
	}
	return d
}

// RateLimited records a 429 and returns when the credential is back.
func (e *Engine) RateLimited(id string, lim Limit) int64 {
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.state(id)
	until := lim.Until
	label := ""
	if lim.Account {
		label = lim.Label
		if label == "" {
			label = "usage limit"
		}
	}
	if until <= now {
		s.Failures++
		until = now + backoff(s.Failures)
	} else if until-now < 10_000 {
		until = now + 10_000
	}
	if until > s.CoolUntil {
		s.CoolUntil = until
		s.CoolSince = now
		s.CoolWindow = label
	}
	e.persist(id, true)
	return s.CoolUntil
}

// AuthRejected rests a credential for 30 minutes after a 401/403 that survived a refresh.
func (e *Engine) AuthRejected(id string) {
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.state(id)
	s.CoolUntil, s.CoolSince, s.CoolWindow = now+30*60*1000, now, ""
	e.persist(id, true)
}

// ClearCooldown ends a cooldown early (panel action and the management API).
func (e *Engine) ClearCooldown(id string) {
	e.mu.Lock()
	s := e.state(id)
	s.CoolUntil, s.CoolWindow, s.CoolSince, s.Failures = 0, "", 0, 0
	e.persist(id, true)
	e.mu.Unlock()
}

// Candidate is a credential the router may try, in order.
type Candidate struct {
	Credential domain.Credential
	Bound      bool // the session is already pinned here
}

// Order filters creds to the usable ones and orders them: the account the
// session is pinned to first, then accounts whose longest window resets soonest
// while it still has reserve, then accounts with no quota data yet (oldest first).
// backAt is the earliest time an unusable credential returns (0 if none will).
func (e *Engine) Order(provider domain.ProviderID, creds []domain.Credential, sessionKey string) (out []Candidate, backAt int64) {
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	bound := ""
	if b, ok := e.bindings[bindKey(provider, sessionKey)]; ok && b.until > now {
		bound = b.credID
	}
	type scored struct {
		c       domain.Credential
		known   bool
		resetAt int64
	}
	var list []scored
	var pinned *domain.Credential
	for _, c := range creds {
		st := State{}
		if s := e.states[c.ID]; s != nil {
			st = *s
		}
		if !st.Usable(now) {
			if b := st.BackAt(now); b > 0 && (backAt == 0 || b < backAt) {
				backAt = b
			}
			continue
		}
		if c.ID == bound {
			cp := c
			pinned = &cp
			continue
		}
		sc := scored{c: c}
		if w, ok := st.LongWindow(now); ok && w.Left() != 0 {
			sc.known, sc.resetAt = true, *w.ResetsAt
		}
		list = append(list, sc)
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.known != b.known {
			return a.known
		}
		if a.known && a.resetAt != b.resetAt {
			return a.resetAt < b.resetAt
		}
		return a.c.CreatedAt < b.c.CreatedAt
	})
	if pinned != nil {
		out = append(out, Candidate{Credential: *pinned, Bound: true})
	}
	for _, sc := range list {
		out = append(out, Candidate{Credential: sc.c})
	}
	return out, backAt
}

// NextUp returns the credential a new session would get right now.
func (e *Engine) NextUp(provider domain.ProviderID, creds []domain.Credential) (domain.Credential, bool) {
	order, _ := e.Order(provider, creds, "")
	if len(order) == 0 {
		return domain.Credential{}, false
	}
	return order[0].Credential, true
}

// Bind pins a session to a credential for the affinity window. It returns the
// credential the session was on before when it moved, or "".
func (e *Engine) Bind(provider domain.ProviderID, sessionKey, credID string) (movedFrom string) {
	if sessionKey == "" {
		return ""
	}
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	k := bindKey(provider, sessionKey)
	if old, ok := e.bindings[k]; ok && old.until > now && old.credID != credID {
		movedFrom = old.credID
	}
	e.bindings[k] = binding{credID: credID, until: now + e.ttl.Milliseconds()}
	if len(e.bindings) > 65536 {
		for key, b := range e.bindings {
			if b.until <= now {
				delete(e.bindings, key)
			}
		}
	}
	return movedFrom
}

// BoundTo returns the credential a session is pinned to, if any.
func (e *Engine) BoundTo(provider domain.ProviderID, sessionKey string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if b, ok := e.bindings[bindKey(provider, sessionKey)]; ok && b.until > e.now() {
		return b.credID
	}
	return ""
}

func bindKey(p domain.ProviderID, sessionKey string) string { return string(p) + "|" + sessionKey }

// SetPlan remembers the subscription plan the provider reported.
func (e *Engine) SetPlan(id, plan string) {
	if plan == "" {
		return
	}
	e.mu.Lock()
	e.state(id).Plan = plan
	e.persist(id, true)
	e.mu.Unlock()
}

// Backdate pretends the credential's windows were read at ts (demo data).
func (e *Engine) Backdate(id string, ts int64) {
	e.mu.Lock()
	e.state(id).ObservedAt = ts
	e.persist(id, true)
	e.mu.Unlock()
}
