package domain

import (
	"regexp"
	"strings"
)

// The meter (store.PriceStore) and the model lists (provider) price the same
// ids from different copies of the models.dev catalogue. These helpers hold the
// id rules both use, so a model shows the price it is charged at.

// PriceLookup is one place to look for a price: a models.dev catalogue and an
// id in it.
type PriceLookup struct {
	Catalog string
	ID      string
}

// CursorPriceCatalogs are the models.dev catalogues behind the models Cursor
// resells, in the order they are tried. OpenCode's list goes last: it covers
// models the vendors have retired from their own lists (Sonnet 4) and Kimi.
var CursorPriceCatalogs = []string{"anthropic", "openai", "xai", "google", "zai", "opencode"}

// CursorPriceLookups lists where a Cursor id's price is looked for, in order:
// Cursor's own list, then each vendor catalogue with Cursor's spelling,
// Gemini's "-preview" id and the catalogue spelling of a Claude id. Lookups
// also match the longest catalogue id that the id starts with, so effort and
// -fast suffixes fall back to the base model unless it has its own entry.
func CursorPriceLookups(model string) []PriceLookup {
	ids := []string{model, model + "-preview"}
	if id := CursorClaudeID(model); id != "" {
		ids = append(ids, id)
	}
	out := []PriceLookup{{Catalog: "cursor", ID: model}}
	for _, catalog := range CursorPriceCatalogs {
		for _, id := range ids {
			out = append(out, PriceLookup{Catalog: catalog, ID: id})
		}
	}
	return out
}

var cursorClaudeRe = regexp.MustCompile(`^claude-(\d+(?:\.\d+)?)-(opus|sonnet|haiku)(?:-.*)?$`)

// CursorClaudeID turns Cursor's claude-<version>-<family>[-effort][-thinking]
// [-fast] into the catalogue's claude-<family>-<version>, with dots as dashes.
// It returns "" for ids in any other shape.
func CursorClaudeID(model string) string {
	m := cursorClaudeRe.FindStringSubmatch(model)
	if m == nil {
		return ""
	}
	return "claude-" + m[2] + "-" + strings.ReplaceAll(m[1], ".", "-")
}

// SourcePriceCatalogs are the catalogues searched for a model from an
// OpenAI-compatible source, in the order a shared id resolves to the first.
var SourcePriceCatalogs = []string{"openai", "anthropic", "xai", "google", "zai"}

// SourcePriceLookups lists where a source model's price is looked for: the id
// as given in every catalogue, then the part after the last "/" (routers write
// openai/gpt-5.5). Only whole ids match, so a similar name never borrows
// another model's price.
func SourcePriceLookups(id string) []PriceLookup {
	ids := []string{id}
	if i := strings.LastIndex(id, "/"); i >= 0 && i+1 < len(id) {
		ids = append(ids, id[i+1:])
	}
	var out []PriceLookup
	for _, id := range ids {
		for _, catalog := range SourcePriceCatalogs {
			out = append(out, PriceLookup{Catalog: catalog, ID: id})
		}
	}
	return out
}
