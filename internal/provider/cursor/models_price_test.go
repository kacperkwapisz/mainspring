package cursor

import (
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

// /v1/models shows the price the meter charges for Cursor ids the vendors
// spell differently.
func TestListedCursorPriceMatchesTheMeter(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	prices, err := store.NewPriceStore(d)
	if err != nil {
		t.Fatal(err)
	}
	listed := ToModelInfo([]Model{
		{ID: "claude-4.5-sonnet", Name: "Sonnet 4.5"},
		{ID: "gemini-3.1-pro", Name: "Gemini 3.1 Pro"},
		{ID: "glm-5.2", Name: "GLM 5.2"},
		{ID: "kimi-k2.7-code", Name: "Kimi K2.7 Code"},
	}, false)
	for _, m := range listed {
		meter := prices.Get("cursor", m.ID)
		if meter == nil || meter.Input == nil {
			t.Fatalf("%s: meter has no price", m.ID)
		}
		if m.Cost == nil || m.Cost.Input == nil || *m.Cost.Input != *meter.Input || len(m.Cost.Tiers) != len(meter.Tiers) {
			t.Fatalf("%s: listed %+v, meter %+v", m.ID, m.Cost, meter)
		}
	}
	var gemini = listed[1]
	if gemini.ContextLength != 1048576 || len(gemini.Cost.Tiers) != 1 {
		t.Fatalf("gemini %+v", gemini)
	}
}
