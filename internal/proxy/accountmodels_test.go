package proxy

import (
	"context"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

type liveExec struct {
	fakeExec
	lists map[string][]domain.ModelInfo
}

func (l liveExec) ModelsFor(_ context.Context, c domain.Credential) []domain.ModelInfo {
	return l.lists[c.Label]
}

func TestModelListIsTheUnionOfWhatAccountsOffer(t *testing.T) {
	ex := liveExec{
		fakeExec: fakeExec{id: domain.ProviderClaude, format: domain.FormatClaude, models: []domain.ModelInfo{{ID: "claude-from-catalog"}}},
		lists: map[string][]domain.ModelInfo{
			"team":  {{ID: "claude-fable-5-2"}, {ID: "claude-opus-5-5"}},
			"max":   {{ID: "claude-opus-5-5"}, {ID: "claude-haiku-5-5"}},
			"stale": nil, // its fetch failed
		},
	}
	hub, d := testHub(t, map[domain.ProviderID]provider.Executor{domain.ProviderClaude: ex})
	defer d.Close()
	for _, label := range []string{"team", "max", "stale"} {
		if _, err := hub.Vault.Save(store.SaveCredential{Provider: domain.ProviderClaude, Label: label, Tokens: domain.CredentialTokens{AccessToken: "t"}}); err != nil {
			t.Fatal(err)
		}
	}
	var ids []string
	for _, m := range ListModels(hub) {
		ids = append(ids, m.ID)
	}
	want := map[string]bool{"claude-fable-5-2": true, "claude-opus-5-5": true, "claude-haiku-5-5": true}
	if len(ids) != 3 {
		t.Fatalf("got %v", ids)
	}
	for _, id := range ids {
		if !want[id] {
			t.Fatalf("got %v", ids)
		}
	}

	// No account could be asked: fall back to the catalog.
	ex.lists = map[string][]domain.ModelInfo{}
	hub.Executors[domain.ProviderClaude] = ex
	if got := ListModels(hub); len(got) != 1 || got[0].ID != "claude-from-catalog" {
		t.Fatalf("fallback %v", got)
	}
}
