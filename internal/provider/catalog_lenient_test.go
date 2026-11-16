package provider

import (
	"slices"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/prices"
)

// One model that doesn't fit our types is skipped; the models after it stay.
func TestCatalogSkipsAModelThatDoesNotParse(t *testing.T) {
	defer prices.SetLive(prices.APIJSON)
	trimmed, err := prices.Trim([]byte(`{"anthropic":{"id":"anthropic","name":"Anthropic","models":{
		"claude-new-9":{"id":"claude-new-9","release_date":"2026-12-01","limit":{"context":"a lot"}},
		"claude-mid-8":{"id":"claude-mid-8","release_date":"2026-11-01","cost":{"input":1,"tiers":[{"tier":{"type":"context","size":"200k"}}]}},
		"claude-old-1":{"id":"claude-old-1","release_date":"2025-01-01","limit":{"context":200000,"output":8000}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	prices.SetLive(trimmed)
	var ids []string
	for _, m := range CatalogModels(domain.ProviderClaude) {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{"claude-mid-8", "claude-old-1"}) {
		t.Fatalf("got %v", ids)
	}
}

// Without the account's own list, efforts come from the version: Claude 4.6
// and later have effort, and xhigh is Opus 4.7+ and Fable only.
func TestClaudeCatalogEffortsFollowTheVersion(t *testing.T) {
	full := []string{"low", "medium", "high", "xhigh", "max"}
	noX := []string{"low", "medium", "high", "max"}
	for id, want := range map[string][]string{
		"claude-opus-4-7":            full,
		"claude-opus-4-8":            full,
		"claude-opus-5":              full,
		"claude-opus-5-5":            full,
		"claude-fable-5":             full,
		"claude-fable-5-2":           full,
		"claude-sonnet-4-6":          noX,
		"claude-opus-4-6":            noX,
		"claude-sonnet-5-5":          noX,
		"claude-haiku-5-5":           noX,
		"claude-opus-4-5":            nil,
		"claude-opus-4-5-20251101":   nil,
		"claude-sonnet-4-5-20250929": nil,
		"claude-haiku-4-5":           nil,
		"claude-opus-4-1":            nil,
		"claude-opus-4-20250514":     nil,
		"claude-3-7-sonnet-20250219": nil,
		"claude-3-5-haiku":           nil,
		"claude-4-6-sonnet":          noX,
		"claude-4-7-opus":            full,
		"claude-mystery":             nil,
	} {
		if got := modelEfforts(domain.ProviderClaude, id); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", id, got, want)
		}
	}
	for _, m := range CatalogModels(domain.ProviderClaude) {
		if m.ID == "claude-haiku-4-5" && (len(m.Efforts) != 0 || !m.Reasoning) {
			t.Fatalf("haiku 4.5 %+v", m)
		}
	}
}

// Cursor ids the vendors spell differently still get a price and limits.
func TestCursorLimitsUseTheVendorCatalogues(t *testing.T) {
	for _, tc := range []struct {
		id    string
		input float64
		ctx   int
		tiers int
	}{
		{"claude-4.5-sonnet", 3, 200000, 0},
		{"cursor/claude-4.6-opus", 5, 1000000, 0},
		{"gemini-3.1-pro", 2, 1048576, 1},
		{"glm-5.2", 0, 0, -1},
		{"kimi-k2.7-code", 0, 0, -1},
		{"composer-2.5-fast", 3, 0, 0},
	} {
		ctx, _, cost, _, ok := CursorLimits(tc.id)
		if !ok || cost == nil || cost.Input == nil || *cost.Input <= 0 {
			t.Fatalf("%s: cost %+v ok %v", tc.id, cost, ok)
		}
		if tc.input > 0 && *cost.Input != tc.input {
			t.Errorf("%s: input %v, want %v", tc.id, *cost.Input, tc.input)
		}
		if tc.ctx > 0 && ctx != tc.ctx {
			t.Errorf("%s: context %d, want %d", tc.id, ctx, tc.ctx)
		}
		if tc.tiers >= 0 && len(cost.Tiers) != tc.tiers {
			t.Errorf("%s: tiers %+v", tc.id, cost.Tiers)
		}
	}
	if _, _, cost, _, ok := CursorLimits("no-such-model"); ok || cost != nil {
		t.Fatalf("unknown id priced: %+v", cost)
	}
}
