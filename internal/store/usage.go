package store

import (
	"github.com/kacperkwapisz/mainspring/internal/live"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/domain"
)

const retainMS = 90 * 24 * 60 * 60 * 1000

type UsageTotals struct {
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"promptTokens"`
	CompletionTokens int64   `json:"completionTokens"`
	CacheReadTokens  int64   `json:"cacheReadTokens"`
	CacheWriteTokens int64   `json:"cacheWriteTokens"`
	USD              float64 `json:"usd"`
	Errors           int64   `json:"errors"`
}

type UsageBreakdown struct {
	Key string `json:"key"`
	UsageTotals
}

type UsagePoint struct {
	Day string `json:"day"`
	UsageTotals
}

type UsageStore struct {
	Live *live.Bus // told when a request is recorded
	db   *db.DB
}

func NewUsageStore(d *db.DB) *UsageStore {
	ensureColumn(d, "usage_events", "credential_id", "TEXT")
	ensureColumn(d, "usage_events", "error_status", "INTEGER")
	ensureColumn(d, "usage_events", "error_message", "TEXT")
	return &UsageStore{db: d}
}

// ensureColumn adds a column to an existing table once. SQLite has no
// ADD COLUMN IF NOT EXISTS, so check table_info first.
func ensureColumn(d *db.DB, table, column, typ string) {
	rows, err := d.SQL.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return
	}
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil && name == column {
			rows.Close()
			return
		}
	}
	rows.Close()
	_, _ = d.SQL.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + typ)
}

func (s *UsageStore) Record(event domain.UsageEvent) error {
	var keyID, credID any
	if event.KeyID != "" {
		keyID = event.KeyID
	}
	if event.CredentialID != "" {
		credID = event.CredentialID
	}
	_, err := s.db.SQL.Exec(
		`INSERT INTO usage_events(ts, key_id, credential_id, provider, model, inbound, prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, latency_ms, status, usd, error_status, error_message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.TS, keyID, credID, string(event.Provider), event.Model, string(event.Inbound),
		event.PromptTokens, event.CompletionTokens, event.CacheReadTokens, event.CacheWriteTokens,
		event.LatencyMs, event.Status, event.USD, nullInt(event.ErrorStatus), nullStr(event.ErrorMessage),
	)
	if err == nil {
		s.Live.Publish(live.Meter, live.Pool, live.Keys, live.Sessions)
	}
	return err
}

func (s *UsageStore) Purge() (int64, error) {
	cutoff := nowMs() - retainMS
	res, err := s.db.SQL.Exec(`DELETE FROM usage_events WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *UsageStore) Since(msAgo int64) (UsageTotals, error) {
	from := nowMs() - msAgo
	row := s.db.SQL.QueryRow(
		`SELECT COUNT(*) AS requests,
		        COALESCE(SUM(prompt_tokens),0) AS prompt,
		        COALESCE(SUM(completion_tokens),0) AS completion,
		        COALESCE(SUM(cache_read_tokens),0) AS cache_read,
		        COALESCE(SUM(cache_write_tokens),0) AS cache_write,
		        COALESCE(SUM(usd),0) AS usd,
		        COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),0) AS errors
		 FROM usage_events WHERE ts >= ?`, from)
	return scanTotals(row)
}

func (s *UsageStore) GroupBy(msAgo int64, field string) ([]UsageBreakdown, error) {
	from := nowMs() - msAgo
	col := field
	if field == "key_id" {
		col = "COALESCE(key_id, 'unknown')"
	}
	rows, err := s.db.SQL.Query(
		`SELECT `+col+` AS key,
		        COUNT(*) AS requests,
		        COALESCE(SUM(prompt_tokens),0) AS prompt,
		        COALESCE(SUM(completion_tokens),0) AS completion,
		        COALESCE(SUM(cache_read_tokens),0) AS cache_read,
		        COALESCE(SUM(cache_write_tokens),0) AS cache_write,
		        COALESCE(SUM(usd),0) AS usd,
		        COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),0) AS errors
		 FROM usage_events WHERE ts >= ?
		 GROUP BY `+col+`
		 ORDER BY usd DESC, requests DESC`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageBreakdown
	for rows.Next() {
		var b UsageBreakdown
		if err := rows.Scan(&b.Key, &b.Requests, &b.PromptTokens, &b.CompletionTokens, &b.CacheReadTokens, &b.CacheWriteTokens, &b.USD, &b.Errors); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Daily totals for the last days calendar days in loc, oldest first.
func (s *UsageStore) Daily(days int, loc *time.Location) ([]UsagePoint, error) {
	if days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day()-(days-1), 0, 0, 0, 0, loc)
	until := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
	// Sum per quarter hour in SQL, then put each quarter on its day in loc: every
	// UTC offset is a multiple of 15 minutes, so no quarter spans two days.
	const quarter = 15 * 60 * 1000
	rows, err := s.db.SQL.Query(
		`SELECT ts / ? AS q,
		        COUNT(*) AS requests,
		        COALESCE(SUM(prompt_tokens),0) AS prompt,
		        COALESCE(SUM(completion_tokens),0) AS completion,
		        COALESCE(SUM(cache_read_tokens),0) AS cache_read,
		        COALESCE(SUM(cache_write_tokens),0) AS cache_write,
		        COALESCE(SUM(usd),0) AS usd,
		        COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),0) AS errors
		 FROM usage_events WHERE ts >= ? AND ts < ?
		 GROUP BY q`, quarter, start.UnixMilli(), until.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[string]UsageTotals{}
	for rows.Next() {
		var q int64
		var t UsageTotals
		if err := rows.Scan(&q, &t.Requests, &t.PromptTokens, &t.CompletionTokens, &t.CacheReadTokens, &t.CacheWriteTokens, &t.USD, &t.Errors); err != nil {
			return nil, err
		}
		day := time.UnixMilli(q * quarter).In(loc).Format("2006-01-02")
		d := byDay[day]
		d.Requests += t.Requests
		d.PromptTokens += t.PromptTokens
		d.CompletionTokens += t.CompletionTokens
		d.CacheReadTokens += t.CacheReadTokens
		d.CacheWriteTokens += t.CacheWriteTokens
		d.USD += t.USD
		d.Errors += t.Errors
		byDay[day] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]UsagePoint, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, UsagePoint{Day: day, UsageTotals: byDay[day]})
	}
	return out, nil
}

// TodayTokensForKey counts a key's tokens since midnight in loc.
func (s *UsageStore) TodayTokensForKey(keyID string, loc *time.Location) (int64, error) {
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).UnixMilli()
	var n int64
	err := s.db.SQL.QueryRow(
		`SELECT COALESCE(SUM(prompt_tokens + completion_tokens),0) FROM usage_events WHERE key_id = ? AND ts >= ?`,
		keyID, start,
	).Scan(&n)
	return n, err
}

func scanTotals(row rowScanner) (UsageTotals, error) {
	var t UsageTotals
	err := row.Scan(&t.Requests, &t.PromptTokens, &t.CompletionTokens, &t.CacheReadTokens, &t.CacheWriteTokens, &t.USD, &t.Errors)
	return t, err
}

var groupColumns = map[string]string{
	"provider":         "provider",
	"model":            "model",
	"key_id":           "COALESCE(key_id, '')",
	"credential_id":    "COALESCE(credential_id, '')",
	"provider_model":   "provider || '|' || model",
	"inbound_provider": "COALESCE(inbound, '') || '|' || provider",
}

// GroupFrom groups usage since a unix-ms timestamp by an allowed field.
func (s *UsageStore) GroupFrom(from int64, field string) ([]UsageBreakdown, error) {
	col, ok := groupColumns[field]
	if !ok {
		return nil, nil
	}
	rows, err := s.db.SQL.Query(
		`SELECT `+col+` AS key,
		        COUNT(*), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0),
		        COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_write_tokens),0),
		        COALESCE(SUM(usd),0), COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),0)
		 FROM usage_events WHERE ts >= ? GROUP BY key ORDER BY 7 DESC, 2 DESC`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageBreakdown{}
	for rows.Next() {
		var b UsageBreakdown
		if err := rows.Scan(&b.Key, &b.Requests, &b.PromptTokens, &b.CompletionTokens, &b.CacheReadTokens, &b.CacheWriteTokens, &b.USD, &b.Errors); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// TotalsFrom sums usage since a unix-ms timestamp.
func (s *UsageStore) TotalsFrom(from int64) (UsageTotals, error) {
	row := s.db.SQL.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0),
		        COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_write_tokens),0),
		        COALESCE(SUM(usd),0), COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),0)
		 FROM usage_events WHERE ts >= ?`, from)
	return scanTotals(row)
}

// LastUsed returns the newest request time per LocalKey id.
func (s *UsageStore) LastUsed() (map[string]int64, error) {
	rows, err := s.db.SQL.Query(`SELECT key_id, MAX(ts) FROM usage_events WHERE key_id IS NOT NULL GROUP BY key_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var ts int64
		if rows.Scan(&k, &ts) == nil {
			out[k] = ts
		}
	}
	return out, rows.Err()
}

// FirstEvent returns the oldest recorded request time, or 0.
func (s *UsageStore) FirstEvent() int64 {
	var ts int64
	_ = s.db.SQL.QueryRow(`SELECT COALESCE(MIN(ts),0) FROM usage_events`).Scan(&ts)
	return ts
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Failure is one request that failed, as the Meter's error list shows it.
type Failure struct {
	TS           int64  `json:"ts"`
	KeyID        string `json:"key_id,omitempty"`
	CredentialID string `json:"credential_id,omitempty"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Inbound      string `json:"inbound"`
	LatencyMs    int64  `json:"latency_ms"`
	Status       int    `json:"status,omitempty"`  // HTTP status the client got; 0 when a stream broke off
	Message      string `json:"message,omitempty"` // what the client was told; empty for rows from before 1.5
}

// Failures lists failed requests since from, newest first.
func (s *UsageStore) Failures(from int64, limit int) ([]Failure, error) {
	if limit < 1 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.SQL.Query(
		`SELECT ts, COALESCE(key_id,''), COALESCE(credential_id,''), provider, model, inbound, latency_ms,
		        COALESCE(error_status,0), COALESCE(error_message,'')
		 FROM usage_events WHERE status = 'error' AND ts >= ? ORDER BY ts DESC LIMIT ?`, from, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Failure{}
	for rows.Next() {
		var f Failure
		if err := rows.Scan(&f.TS, &f.KeyID, &f.CredentialID, &f.Provider, &f.Model, &f.Inbound, &f.LatencyMs, &f.Status, &f.Message); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
