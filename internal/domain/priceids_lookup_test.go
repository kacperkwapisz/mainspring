package domain

import (
	"slices"
	"testing"
)

func TestSourcePriceLookupsTryTheWholeIDFirst(t *testing.T) {
	got := SourcePriceLookups("router/anthropic/claude-sonnet-4-6")
	if len(got) != 2*len(SourcePriceCatalogs) {
		t.Fatalf("%+v", got)
	}
	if got[0] != (PriceLookup{"openai", "router/anthropic/claude-sonnet-4-6"}) || got[len(SourcePriceCatalogs)] != (PriceLookup{"openai", "claude-sonnet-4-6"}) {
		t.Fatalf("%+v", got)
	}
	if n := len(SourcePriceLookups("gpt-5.5")); n != len(SourcePriceCatalogs) {
		t.Fatalf("plain id: %d lookups", n)
	}
}

func TestCursorPriceLookupsStartWithCursorsOwnList(t *testing.T) {
	got := CursorPriceLookups("claude-4.5-sonnet")
	if got[0] != (PriceLookup{"cursor", "claude-4.5-sonnet"}) {
		t.Fatalf("%+v", got[0])
	}
	if !slices.Contains(got, PriceLookup{"anthropic", "claude-sonnet-4-5"}) || !slices.Contains(got, PriceLookup{"google", "claude-4.5-sonnet-preview"}) {
		t.Fatalf("%+v", got)
	}
}
