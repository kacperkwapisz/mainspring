package store

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/prices"
)

// A models.dev download with one odd tier and one odd model still refreshes
// every other model's price.
func TestRefreshSurvivesOddEntries(t *testing.T) {
	body := `{
	  "anthropic":{"id":"anthropic","name":"Anthropic","models":{
	    "claude-test-odd":{"id":"claude-test-odd","cost":{"input":"free"}},
	    "claude-test-1":{"id":"claude-test-1","cost":{"input":3,"output":15,"tiers":[{"input":6,"tier":{"type":"context","size":"200k"}}]}}}},
	  "google":{"id":"google","name":"Google","models":{
	    "gemini-test-1":{"id":"gemini-test-1","cost":{"input":2,"output":12,"tiers":{"input":4}}},
	    "gemini-test-2":{"id":"gemini-test-2","cost":{"input":1,"output":5,"tiers":[{"input":2,"tier":{"type":"context","size":200000.5}}]}}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	defer prices.SetLive(prices.APIJSON) // Refresh also replaces the model list
	p := priceStore(t)
	p.sourceURL = srv.URL
	if status, err := p.Refresh(true); status != "ok" || err != nil {
		t.Fatalf("refresh %s %v", status, err)
	}
	for _, tc := range []struct {
		provider, model string
		input           float64
		tiers           int
	}{
		{"anthropic", "claude-test-1", 3, 0},
		{"google", "gemini-test-1", 2, 0},
		{"google", "gemini-test-2", 1, 1},
	} {
		got := p.Get(tc.provider, tc.model)
		if got == nil || got.Input == nil || *got.Input != tc.input || len(got.Tiers) != tc.tiers {
			t.Fatalf("%s: %+v", tc.model, got)
		}
	}
	if got := p.Get("google", "gemini-test-2"); got.Tiers[0].InputTokensAbove != 200000 {
		t.Fatalf("tier %+v", got.Tiers)
	}
}

func TestRefreshSaysWhyItFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", http.StatusBadGateway) }))
	defer srv.Close()
	p := priceStore(t)
	p.sourceURL = srv.URL
	if status, err := p.Refresh(true); status != "failed" || err == nil {
		t.Fatalf("refresh %s %v", status, err)
	}
}

// EstimateSource matches whole ids only, the id first and then its last part.
func TestEstimateSourceUsesWholeIDs(t *testing.T) {
	p := priceStore(t)
	u := PriceUsage{Input: 1_000_000}
	if got := p.EstimateSource("router/anthropic/claude-sonnet-4-6", u); got <= 0 {
		t.Fatalf("claude via a router: %v", got)
	}
	if got, want := p.EstimateSource("router/gpt-5.5", u), p.Estimate("openai", "gpt-5.5", u); got != want || got == 0 {
		t.Fatalf("gpt-5.5 %v want %v", got, want)
	}
	if got := p.EstimateSource("router/gpt-5.5-made-up", u); got != 0 {
		t.Fatalf("a similar name borrowed a price: %v", got)
	}
}
