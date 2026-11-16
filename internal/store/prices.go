package store

import (
	"database/sql"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/prices"
)

const (
	modelsDevURL = "https://models.dev/api.json"
	refreshMS    = 24 * 60 * 60 * 1000
)

type PriceCatalog struct {
	Models map[string]PriceModel `json:"models"`
}

type PriceModel struct {
	ID   string     `json:"id"`
	Name string     `json:"name"`
	Cost *PriceCost `json:"cost"`
}

// PriceCost reads a models.dev cost block, tiers included.
type PriceCost = domain.ModelCostJSON

type PriceStore struct {
	db        *db.DB
	lastFetch int64
}

func NewPriceStore(d *db.DB) (*PriceStore, error) {
	s := &PriceStore{db: d}
	// Databases made before price tiers lack the column. Their rows have no
	// tiers either, so reload the snapshot once the column exists.
	hadTiers := hasColumn(d, "prices", "tiers")
	ensureColumn(d, "prices", "tiers", "TEXT")
	if err := s.seedFromSnapshot(!hadTiers); err != nil {
		return nil, err
	}
	// Start from the last downloaded model list, if there is one.
	if _, err := d.SQL.Exec(`CREATE TABLE IF NOT EXISTS catalog_snapshot (id INTEGER PRIMARY KEY, body TEXT NOT NULL, fetched_at INTEGER NOT NULL)`); err != nil {
		return nil, err
	}
	var body string
	if d.SQL.QueryRow(`SELECT body FROM catalog_snapshot WHERE id = 1`).Scan(&body) == nil {
		prices.SetLive([]byte(body))
	}
	return s, nil
}

func (s *PriceStore) seedFromSnapshot(force bool) error {
	var n int
	if err := s.db.SQL.QueryRow(`SELECT COUNT(*) FROM prices`).Scan(&n); err != nil {
		return err
	}
	if n == 0 || force {
		merged := map[string]PriceCatalog{}
		if err := json.Unmarshal(prices.APIJSON, &merged); err != nil {
			return err
		}
		if err := s.upsertCatalog(merged, nowMs()); err != nil {
			return err
		}
	}
	if err := s.upsertCursorOverlay(); err != nil {
		return err
	}
	return s.upsertOpenCodeOverlay()
}

func (s *PriceStore) upsertCursorOverlay() error {
	cursor := map[string]PriceCatalog{}
	if err := json.Unmarshal(prices.CursorJSON, &cursor); err != nil {
		return err
	}
	if err := s.upsertCatalog(cursor, nowMs()); err != nil {
		return err
	}
	_, err := s.db.SQL.Exec(`DELETE FROM prices WHERE provider = 'cursor' AND model IN ('cursor-auto', 'auto', 'default')`)
	return err
}

func (s *PriceStore) upsertOpenCodeOverlay() error {
	opencode := map[string]PriceCatalog{}
	if err := json.Unmarshal(prices.OpenCodeJSON, &opencode); err != nil {
		return err
	}
	return s.upsertCatalog(opencode, nowMs())
}

func (s *PriceStore) Get(provider, model string) *domain.ModelPrice {
	if provider == "cursor" && isCursorAuto(model) {
		return nil
	}
	if p := s.lookup(provider, model); p != nil {
		return p
	}
	if provider == "cursor" {
		return s.cursorFallback(model)
	}
	return nil
}

// cursorCatalogs are the models.dev providers behind the models Cursor resells,
// in the order they are tried. OpenCode's list goes last: it covers models the
// vendors have retired from their own lists (Sonnet 4) and Kimi.
var cursorCatalogs = []string{"anthropic", "openai", "xai", "google", "zai", "opencode"}

// cursorFallback prices a Cursor id from the vendors' catalogues. It tries
// Cursor's own spelling, then Gemini's "-preview" id, then the catalogue
// spelling of a Claude id.
func (s *PriceStore) cursorFallback(model string) *domain.ModelPrice {
	ids := []string{model, model + "-preview"}
	if id := cursorClaudeID(model); id != "" {
		ids = append(ids, id)
	}
	for _, catalog := range cursorCatalogs {
		for _, id := range ids {
			if p := s.lookup(catalog, id); p != nil {
				return p
			}
		}
	}
	return nil
}

var cursorClaudeRe = regexp.MustCompile(`^claude-(\d+(?:\.\d+)?)-(opus|sonnet|haiku)(?:-.*)?$`)

// cursorClaudeID turns Cursor's claude-<version>-<family>[-effort][-thinking]
// [-fast] into the catalogue's claude-<family>-<version>, with dots as dashes.
// It returns "" for ids in any other shape.
func cursorClaudeID(model string) string {
	m := cursorClaudeRe.FindStringSubmatch(model)
	if m == nil {
		return ""
	}
	return "claude-" + m[2] + "-" + strings.ReplaceAll(m[1], ".", "-")
}

func (s *PriceStore) lookup(provider, model string) *domain.ModelPrice {
	row := s.db.SQL.QueryRow(`SELECT provider, model, input, output, cache_read, cache_write, tiers FROM prices WHERE provider = ? AND model = ?`, provider, model)
	if p, err := scanPrice(row); err == nil {
		return &p
	}
	row = s.db.SQL.QueryRow(
		`SELECT provider, model, input, output, cache_read, cache_write, tiers FROM prices WHERE provider = ? AND (model = ? OR ? LIKE model || '%') ORDER BY length(model) DESC LIMIT 1`,
		provider, model, model,
	)
	if p, err := scanPrice(row); err == nil {
		return &p
	}
	return nil
}

// PriceUsage is one request's tokens, split the way prices apply to them.
// Input excludes cached tokens (Anthropic's convention); callers normalise
// OpenAI-style counts, whose prompt tokens include the cache, before pricing.
type PriceUsage struct {
	Input        int64 // uncached input tokens
	Output       int64
	CacheRead    int64
	CacheWrite   int64 // all cache writes, CacheWrite1h included
	CacheWrite1h int64 // the part of CacheWrite written with Claude's one-hour TTL
}

// Estimate prices a request at the model's API list prices, in USD. When the
// total input (uncached, cache reads and cache writes) passes a long-context
// tier's threshold, the whole request is priced at that tier's rates. One-hour
// cache writes cost twice the input price, as Anthropic charges them.
func (s *PriceStore) Estimate(provider, model string, u PriceUsage) float64 {
	p := s.Get(provider, model)
	if p == nil {
		return 0
	}
	rate := effectiveRates(p, u.Input+u.CacheRead+u.CacheWrite)
	perM := 1_000_000.0
	usd := 0.0
	long := min(max(u.CacheWrite1h, 0), u.CacheWrite)
	if rate.Input != nil {
		usd += *rate.Input * float64(u.Input) / perM
		usd += *rate.Input * 2 * float64(long) / perM
	}
	if rate.Output != nil {
		usd += *rate.Output * float64(u.Output) / perM
	}
	if rate.CacheRead != nil {
		usd += *rate.CacheRead * float64(u.CacheRead) / perM
	}
	if rate.CacheWrite != nil {
		usd += *rate.CacheWrite * float64(u.CacheWrite-long) / perM
	}
	return math.Round(usd*1_000_000) / 1_000_000
}

// effectiveRates returns the rates for a request with totalInput input tokens:
// the base rates, with the fields of the highest tier it exceeds laid over them.
func effectiveRates(p *domain.ModelPrice, totalInput int64) domain.ModelCostJSON {
	rate := domain.ModelCostJSON{Input: p.Input, Output: p.Output, CacheRead: p.CacheRead, CacheWrite: p.CacheWrite}
	var tier *domain.ModelCostTier
	for i := range p.Tiers {
		t := &p.Tiers[i]
		if totalInput > int64(t.InputTokensAbove) && (tier == nil || t.InputTokensAbove > tier.InputTokensAbove) {
			tier = t
		}
	}
	if tier == nil {
		return rate
	}
	if tier.Input != nil {
		rate.Input = tier.Input
	}
	if tier.Output != nil {
		rate.Output = tier.Output
	}
	if tier.CacheRead != nil {
		rate.CacheRead = tier.CacheRead
	}
	if tier.CacheWrite != nil {
		rate.CacheWrite = tier.CacheWrite
	}
	return rate
}

func (s *PriceStore) Refresh(force bool) string {
	now := nowMs()
	if !force && now-s.lastFetch < refreshMS {
		return "skipped"
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Get(modelsDevURL)
	if err != nil {
		return "failed"
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "failed"
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "failed"
	}
	var data map[string]PriceCatalog
	if err := json.Unmarshal(body, &data); err != nil {
		return "failed"
	}
	// The same download keeps the model list current.
	if trimmed, err := prices.Trim(body); err == nil {
		prices.SetLive(trimmed)
		_, _ = s.db.SQL.Exec(`INSERT INTO catalog_snapshot(id, body, fetched_at) VALUES (1, ?, ?)
		  ON CONFLICT(id) DO UPDATE SET body = excluded.body, fetched_at = excluded.fetched_at`, string(trimmed), now)
	}
	if err := s.upsertCatalog(data, now); err != nil {
		return "failed"
	}
	if err := s.upsertCursorOverlay(); err != nil {
		return "failed"
	}
	if err := s.upsertOpenCodeOverlay(); err != nil {
		return "failed"
	}
	s.lastFetch = now
	return "ok"
}

func (s *PriceStore) upsertCatalog(data map[string]PriceCatalog, fetchedAt int64) error {
	tx, err := s.db.SQL.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT INTO prices(provider, model, input, output, cache_read, cache_write, tiers, fetched_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(provider, model) DO UPDATE SET
		   input = excluded.input,
		   output = excluded.output,
		   cache_read = excluded.cache_read,
		   cache_write = excluded.cache_write,
		   tiers = excluded.tiers,
		   fetched_at = excluded.fetched_at`,
	)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for provider, catalog := range data {
		for id, m := range catalog.Models {
			model := m.ID
			if model == "" {
				model = id
			}
			var in, out, cr, cw, tiers any
			if m.Cost != nil {
				in, out, cr, cw = m.Cost.Input, m.Cost.Output, m.Cost.CacheRead, m.Cost.CacheWrite
				if len(m.Cost.Tiers) > 0 {
					raw, err := json.Marshal(m.Cost.Tiers)
					if err != nil {
						tx.Rollback()
						return err
					}
					tiers = string(raw)
				}
			}
			if _, err := stmt.Exec(provider, model, in, out, cr, cw, tiers, fetchedAt); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

func isCursorAuto(model string) bool {
	switch model {
	case "cursor-auto", "auto", "default":
		return true
	default:
		return false
	}
}

func scanPrice(row rowScanner) (domain.ModelPrice, error) {
	var p domain.ModelPrice
	var in, out, cr, cw sql.NullFloat64
	var tiers sql.NullString
	err := row.Scan(&p.Provider, &p.Model, &in, &out, &cr, &cw, &tiers)
	if err != nil {
		return p, err
	}
	if in.Valid {
		p.Input = &in.Float64
	}
	if out.Valid {
		p.Output = &out.Float64
	}
	if cr.Valid {
		p.CacheRead = &cr.Float64
	}
	if cw.Valid {
		p.CacheWrite = &cw.Float64
	}
	if tiers.Valid && tiers.String != "" {
		// A bad value only costs the tiers, not the whole price.
		_ = json.Unmarshal([]byte(tiers.String), &p.Tiers)
		sort.SliceStable(p.Tiers, func(i, j int) bool { return p.Tiers[i].InputTokensAbove < p.Tiers[j].InputTokensAbove })
	}
	return p, nil
}

func hasColumn(d *db.DB, table, column string) bool {
	rows, err := d.SQL.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil && name == column {
			return true
		}
	}
	return false
}
