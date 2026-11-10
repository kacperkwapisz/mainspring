package store

import (
	"encoding/json"
	"github.com/kacperkwapisz/mainspring/internal/live"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/db"
)

// Activity kinds, rendered by the panel.
const (
	ActivityLimit     = "limit"      // an account ran out of a quota window
	ActivityMoved     = "moved"      // sessions moved to another account
	ActivitySignIn    = "sign_in"    // the provider rejected a refresh token
	ActivityAllOut    = "all_out"    // no account of a provider could serve
	ActivityAdded     = "added"      // an account or source was added or re-signed in
	ActivityRemoved   = "removed"    // an account or source was disconnected
	ActivityBenched   = "benched"    // taken out of or put back into rotation
	ActivityKeyAdded  = "key_added"  // a LocalKey was created
	ActivityKeyRevoke = "key_revoke" // a LocalKey was revoked
)

// ActivityEvent is one thing Mainspring did or noticed.
type ActivityEvent struct {
	ID       int64          `json:"id"`
	TS       int64          `json:"ts"`
	Kind     string         `json:"kind"`
	Provider string         `json:"provider,omitempty"`
	CredID   string         `json:"credential_id,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

type ActivityStore struct {
	db   *db.DB
	Live *live.Bus // told when an event is added
}

func NewActivityStore(d *db.DB) *ActivityStore {
	_, _ = d.SQL.Exec(`CREATE TABLE IF NOT EXISTS activity (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  ts INTEGER NOT NULL,
	  kind TEXT NOT NULL,
	  provider TEXT,
	  credential_id TEXT,
	  data TEXT
	)`)
	_, _ = d.SQL.Exec(`CREATE INDEX IF NOT EXISTS activity_ts ON activity(ts)`)
	return &ActivityStore{db: d}
}

// Add records an event and returns its id.
func (s *ActivityStore) Add(kind, provider, credID string, data map[string]any) int64 {
	raw, _ := json.Marshal(data)
	res, err := s.db.SQL.Exec(`INSERT INTO activity(ts, kind, provider, credential_id, data) VALUES (?, ?, ?, ?, ?)`,
		time.Now().UnixMilli(), kind, provider, credID, string(raw))
	if err != nil {
		return 0
	}
	s.Live.Publish(live.Pool)
	id, _ := res.LastInsertId()
	return id
}

// LatestFor returns the newest event of kind for a credential within msAgo.
func (s *ActivityStore) LatestFor(kind, credID string, msAgo int64) (ActivityEvent, bool) {
	row := s.db.SQL.QueryRow(`SELECT id, ts, kind, COALESCE(provider,''), COALESCE(credential_id,''), COALESCE(data,'{}')
	  FROM activity WHERE kind = ? AND credential_id = ? AND ts >= ? ORDER BY ts DESC LIMIT 1`,
		kind, credID, time.Now().UnixMilli()-msAgo)
	ev, err := scanActivity(row.Scan)
	return ev, err == nil
}

// Update replaces an event's data.
func (s *ActivityStore) Update(id int64, data map[string]any) {
	raw, _ := json.Marshal(data)
	_, _ = s.db.SQL.Exec(`UPDATE activity SET data = ? WHERE id = ?`, string(raw), id)
	s.Live.Publish(live.Pool)
}

// Since lists events newer than ts, newest first, at most limit.
func (s *ActivityStore) Since(ts int64, limit int) ([]ActivityEvent, error) {
	rows, err := s.db.SQL.Query(`SELECT id, ts, kind, COALESCE(provider,''), COALESCE(credential_id,''), COALESCE(data,'{}')
	  FROM activity WHERE ts >= ? ORDER BY ts DESC LIMIT ?`, ts, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityEvent{}
	for rows.Next() {
		ev, err := scanActivity(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// Purge drops events older than 30 days.
func (s *ActivityStore) Purge() {
	_, _ = s.db.SQL.Exec(`DELETE FROM activity WHERE ts < ?`, time.Now().UnixMilli()-30*24*60*60*1000)
}

func scanActivity(scan func(...any) error) (ActivityEvent, error) {
	var ev ActivityEvent
	var raw string
	if err := scan(&ev.ID, &ev.TS, &ev.Kind, &ev.Provider, &ev.CredID, &raw); err != nil {
		return ev, err
	}
	_ = json.Unmarshal([]byte(raw), &ev.Data)
	return ev, nil
}

// AddAt records an event with an explicit timestamp (demo data and imports).
func (s *ActivityStore) AddAt(ts int64, kind, provider, credID string, data map[string]any) int64 {
	raw, _ := json.Marshal(data)
	res, err := s.db.SQL.Exec(`INSERT INTO activity(ts, kind, provider, credential_id, data) VALUES (?, ?, ?, ?, ?)`,
		ts, kind, provider, credID, string(raw))
	if err != nil {
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}
