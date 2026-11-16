package store

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/db"
)

func priceStore(t *testing.T) *PriceStore {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	p, err := NewPriceStore(d)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// claude-haiku-5-5 costs 0.1/0.5/0.01/0.125 per million, and 0.5/2.5/0.05/0.625
// once a request's input passes 100k tokens.
func TestEstimateUsesTheTierPastItsThreshold(t *testing.T) {
	p := priceStore(t)
	tests := []struct {
		name string
		u    PriceUsage
		want float64
	}{
		{"50k input stays on base rates", PriceUsage{Input: 50_000, Output: 1_000_000}, 0.1*0.05 + 0.5},
		{"150k input uses the tier", PriceUsage{Input: 150_000, Output: 1_000_000}, 0.5*0.15 + 2.5},
		{"exactly 100k is not above the tier", PriceUsage{Input: 100_000}, 0.1 * 0.1},
		{"100k and 1 token is", PriceUsage{Input: 100_001}, 0.5 * 0.100001},
		{"cache reads and writes count towards the threshold", PriceUsage{Input: 10_000, CacheRead: 90_000, CacheWrite: 50_000},
			0.5*0.01 + 0.05*0.09 + 0.625*0.05},
		{"a one-hour write costs twice the tier input price", PriceUsage{Input: 0, CacheWrite: 200_000, CacheWrite1h: 200_000}, 2 * 0.5 * 0.2},
		{"a one-hour write costs twice the base input price below the tier", PriceUsage{CacheWrite: 50_000, CacheWrite1h: 50_000}, 2 * 0.1 * 0.05},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.Estimate("anthropic", "claude-haiku-5-5", tc.u); !near(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// gpt-5.5 has no cache_write rate in its 272k tier or its base, and its tier
// has all the other rates. A rate a tier leaves out keeps the base value.
func TestEstimateTierFallsBackToBaseForMissingRates(t *testing.T) {
	p := priceStore(t)
	got := p.Estimate("openai", "gpt-5.5", PriceUsage{Input: 300_000, Output: 100_000, CacheRead: 100_000})
	want := 10*0.3 + 45*0.1 + 1*0.1
	if !near(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if base := p.Estimate("openai", "gpt-5.5", PriceUsage{Input: 200_000, Output: 100_000}); !near(base, 5*0.2+30*0.1) {
		t.Fatalf("base %v", base)
	}
}

func TestTiersSurviveTheDatabaseAndPickTheHighestExceeded(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := NewPriceStore(d)
	if err != nil {
		t.Fatal(err)
	}
	cat := map[string]PriceCatalog{}
	if err := jsonUnmarshal(`{"x":{"models":{"m":{"id":"m","cost":{"input":1,"output":1,
	  "tiers":[
	    {"input":3,"output":3,"tier":{"type":"context","size":200000}},
	    {"input":2,"output":2,"tier":{"type":"context","size":100000}},
	    {"input":99,"output":99,"tier":{"type":"time","size":1}}
	  ]}}}}}`, &cat); err != nil {
		t.Fatal(err)
	}
	if err := p.upsertCatalog(cat, 1); err != nil {
		t.Fatal(err)
	}
	got := p.Get("x", "m")
	if got == nil || len(got.Tiers) != 2 || got.Tiers[0].InputTokensAbove != 100000 || got.Tiers[1].InputTokensAbove != 200000 {
		t.Fatalf("tiers %+v", got)
	}
	for _, tc := range []struct {
		in   int64
		want float64
	}{{50_000, 1}, {150_000, 2}, {250_000, 3}} {
		if usd := p.Estimate("x", "m", PriceUsage{Input: tc.in}); !near(usd, tc.want*float64(tc.in)/1e6) {
			t.Fatalf("input %d: %v", tc.in, usd)
		}
	}
}

// A database from before tiers has no tiers column and untiered rows.
func TestOldPricesTableGainsTiers(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, q := range []string{
		`DROP TABLE prices`,
		`CREATE TABLE prices (provider TEXT NOT NULL, model TEXT NOT NULL, input REAL, output REAL, cache_read REAL, cache_write REAL, fetched_at INTEGER NOT NULL, PRIMARY KEY (provider, model))`,
		`INSERT INTO prices VALUES ('anthropic', 'claude-haiku-5-5', 0.1, 0.5, 0.01, 0.125, 1)`,
	} {
		if _, err := d.SQL.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	p, err := NewPriceStore(d)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Estimate("anthropic", "claude-haiku-5-5", PriceUsage{Input: 150_000}); !near(got, 0.5*0.15) {
		t.Fatalf("got %v", got)
	}
}

// Every id here missed every catalogue before: Cursor spells Claude
// claude-<version>-<family>, the catalogue claude-<family>-<version>.
func TestCursorIDsPriceFromTheCatalogues(t *testing.T) {
	p := priceStore(t)
	tests := []struct{ id, provider, model string }{
		{"claude-4-sonnet", "opencode", "claude-sonnet-4"},
		{"claude-4-sonnet-thinking", "opencode", "claude-sonnet-4"},
		{"claude-4.5-opus-high", "anthropic", "claude-opus-4-5"},
		{"claude-4.5-opus-high-thinking", "anthropic", "claude-opus-4-5"},
		{"claude-4.5-sonnet", "anthropic", "claude-sonnet-4-5"},
		{"claude-4.5-sonnet-thinking", "anthropic", "claude-sonnet-4-5"},
		{"claude-4.6-opus-high", "anthropic", "claude-opus-4-6"},
		{"claude-4.6-opus-max", "anthropic", "claude-opus-4-6"},
		{"claude-4.6-opus-max-thinking", "anthropic", "claude-opus-4-6"},
		{"claude-4.6-sonnet-medium", "anthropic", "claude-sonnet-4-6"},
		{"claude-4.6-sonnet-medium-thinking", "anthropic", "claude-sonnet-4-6"},
		{"claude-fable-5-high", "anthropic", "claude-fable-5"},
		{"claude-opus-4-7-high-fast", "anthropic", "claude-opus-4-7"},
		{"gemini-3-flash", "google", "gemini-3-flash-preview"},
		{"gemini-3.1-pro", "google", "gemini-3.1-pro-preview"},
		{"gemini-3.5-flash", "google", "gemini-3.5-flash"},
		{"gemini-3.6-flash-high", "google", "gemini-3.6-flash"},
		{"glm-5.2-high", "zai", "glm-5.2"},
		{"glm-5.2-max", "zai", "glm-5.2"},
		{"kimi-k2.7-code", "opencode", "kimi-k2.7-code"},
		{"gpt-5.5-extra-high", "openai", "gpt-5.5"},
		{"gpt-5.4-high-fast", "openai", "gpt-5.4"},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			got := p.Get("cursor", tc.id)
			if got == nil || got.Provider != tc.provider || got.Model != tc.model {
				t.Fatalf("got %+v want %s/%s", got, tc.provider, tc.model)
			}
			if got.Input == nil || *got.Input <= 0 {
				t.Fatalf("no input price: %+v", got)
			}
			if p.Estimate("cursor", tc.id, PriceUsage{Input: 1000, Output: 1000}) <= 0 {
				t.Fatal("prices at $0")
			}
		})
	}
	if p.Get("cursor", "auto") != nil || p.Get("cursor", "cursor-auto") != nil {
		t.Fatal("auto must stay unpriced")
	}
}

// composer-2.5-fast has its own price (3/15); composer-2.5 is 0.5/2.5.
func TestCursorFastModelKeepsItsOwnPrice(t *testing.T) {
	p := priceStore(t)
	fast := p.Estimate("cursor", "composer-2.5-fast", PriceUsage{Input: 1_000_000, Output: 1_000_000})
	slow := p.Estimate("cursor", "composer-2.5", PriceUsage{Input: 1_000_000, Output: 1_000_000})
	if !near(fast, 18) || !near(slow, 3) {
		t.Fatalf("fast %v slow %v", fast, slow)
	}
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
