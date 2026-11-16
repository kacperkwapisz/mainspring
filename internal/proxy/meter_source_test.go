package proxy

import (
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/translate"
)

// An OpenAI-compatible source's model is charged the price its model list
// shows, and nothing when the list shows none.
func TestSourceModelMeterMatchesItsListedPrice(t *testing.T) {
	catalog := provider.NewCatalogIndex()
	for _, model := range []string{
		"router/anthropic/claude-sonnet-4-6",
		"router/openai/gpt-5.5",
		"router/gpt-5.5",
		"router/google/gemini-3.1-pro-preview",
		"router/gpt-5.5-made-up",
	} {
		t.Run(model, func(t *testing.T) {
			hub, d := testHub(t, nil)
			defer d.Close()
			created, err := hub.Keys.Create("t", nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			key, _ := hub.Keys.Verify(created.Secret)
			record(hub, Request{Key: *key}, domain.ProviderOpenAI, model, "acct", 0, "ok", translate.Usage{Input: 10_000, Output: 10_000}, "")
			got := meterToday(t, hub).USD
			want := 0.0
			if known, ok := catalog.Find(model); ok && known.Cost != nil {
				want = (*known.Cost.Input + *known.Cost.Output) / 100 // 10k tokens each, priced per million
			}
			if !near(got, want) {
				t.Fatalf("meter %v, listed %v", got, want)
			}
			if model != "router/gpt-5.5-made-up" && got == 0 {
				t.Fatal("charged nothing")
			}
		})
	}
}
