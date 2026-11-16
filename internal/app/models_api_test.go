package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider/opencode"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

func TestModelsNameTheirAPI(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	for _, p := range []domain.ProviderID{domain.ProviderClaude, domain.ProviderCodex, domain.ProviderGrok} {
		if _, err := booted.Hub.Vault.Save(store.SaveCredential{
			Provider: p, Label: string(p),
			Tokens: domain.CredentialTokens{AccessToken: "t", Extra: map[string]any{}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	created, err := booted.Hub.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("authorization", "Bearer "+created.Secret)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d", res.Code)
	}
	var body struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
			API     string `json:"api"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		string(domain.ProviderClaude): "anthropic-messages",
		string(domain.ProviderCodex):  "openai-responses",
		string(domain.ProviderGrok):   "openai-responses",
	}
	seen := map[string]bool{}
	for _, m := range body.Data {
		if m.API == "" {
			t.Errorf("%s has no api", m.ID)
		}
		if w, ok := want[m.OwnedBy]; ok {
			seen[m.OwnedBy] = true
			if m.API != w {
				t.Errorf("%s (%s): api %q, want %q", m.ID, m.OwnedBy, m.API, w)
			}
		}
	}
	for owner := range want {
		if !seen[owner] {
			t.Errorf("no %s model listed", owner)
		}
	}
}

func TestModelAPIPicksTheProvidersWireFormat(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	// One OpenCode model per upstream family, taken from its catalog.
	byFamily := map[opencode.Family]string{}
	for _, m := range opencode.CatalogModels() {
		f := opencode.FamilyOf(m.ID)
		if _, ok := byFamily[f]; !ok {
			byFamily[f] = m.ID
		}
	}
	for f, want := range map[opencode.Family]string{
		opencode.FamilyMessages:  "anthropic-messages",
		opencode.FamilyResponses: "openai-responses",
		opencode.FamilyChat:      "openai-completions",
	} {
		id := byFamily[f]
		if id == "" {
			t.Fatalf("OpenCode catalog has no %s model", f)
		}
		if got := modelAPI(booted.Hub, domain.ModelInfo{ID: id, OwnedBy: "opencode"}); got != want {
			t.Errorf("%s (%s): %q, want %q", id, f, got, want)
		}
	}
	for _, c := range []struct {
		name string
		m    domain.ModelInfo
		want string
	}{
		{"claude", domain.ModelInfo{ID: "claude-opus-4-7", OwnedBy: "claude"}, "anthropic-messages"},
		{"codex", domain.ModelInfo{ID: "gpt-5.5", OwnedBy: "codex"}, "openai-responses"},
		{"grok", domain.ModelInfo{ID: "grok-4.6", OwnedBy: "grok"}, "openai-responses"},
		// Cursor takes chat-shaped requests, whatever model family the ID names.
		{"cursor claude", domain.ModelInfo{ID: "claude-4.6-sonnet", OwnedBy: "cursor"}, "openai-completions"},
		{"cursor prefixed", domain.ModelInfo{ID: "cursor/gpt-5.5", OwnedBy: "cursor"}, "openai-completions"},
		// A source names its own owner, so a prefixed ID never counts as built in.
		{"source claiming claude", domain.ModelInfo{ID: "mine/claude-x", OwnedBy: "claude"}, "openai-completions"},
		{"source", domain.ModelInfo{ID: "mine/llama", OwnedBy: "meta"}, "openai-completions"},
	} {
		if got := modelAPI(booted.Hub, c.m); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
