package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
)

func TestParseModelListPrefixesIDs(t *testing.T) {
	models := parseModelList("openrouter", map[string]any{
		"data": []any{
			map[string]any{"id": "gpt-4o", "name": "GPT-4o"},
			map[string]any{"id": "openrouter/claude-3.5", "owned_by": "anthropic"},
			map[string]any{"id": "gpt-4o"},
		},
	})
	if len(models) != 2 {
		t.Fatalf("len %d", len(models))
	}
	if models[0].ID != "openrouter/gpt-4o" || models[0].Name != "GPT-4o" {
		t.Fatalf("%+v", models[0])
	}
	if models[1].ID != "openrouter/openrouter/claude-3.5" || models[1].OwnedBy != "anthropic" {
		t.Fatalf("%+v", models[1])
	}
}

func TestParseModelListFillsCostFromTheCatalogue(t *testing.T) {
	models := parseModelList("src", map[string]any{
		"data": []any{
			map[string]any{"id": "gpt-5.5"},
			map[string]any{"id": "anthropic/claude-haiku-5-5", "context_length": 123456.0},
			map[string]any{"id": "llama3"},
			map[string]any{"id": "gpt-5.5-mystery"},
		},
	})
	byID := map[string]domain.ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}
	gpt := byID["src/gpt-5.5"]
	if gpt.Cost == nil || gpt.Cost.Input == nil || *gpt.Cost.Input != 5 || len(gpt.Cost.Tiers) != 1 || gpt.Cost.Tiers[0].InputTokensAbove != 272000 {
		t.Fatalf("gpt-5.5 cost %+v", gpt.Cost)
	}
	if gpt.ContextLength == 0 || gpt.MaxOutputTokens == 0 {
		t.Fatalf("gpt-5.5 limits %+v", gpt)
	}
	haiku := byID["src/anthropic/claude-haiku-5-5"]
	if haiku.Cost == nil || haiku.Cost.Input == nil || *haiku.Cost.Input != 0.1 {
		t.Fatalf("haiku cost %+v", haiku.Cost)
	}
	if haiku.ContextLength != 123456 {
		t.Fatalf("the source's own context length was overwritten: %d", haiku.ContextLength)
	}
	for _, id := range []string{"src/llama3", "src/gpt-5.5-mystery"} {
		if m := byID[id]; m.Cost != nil || m.ContextLength != 0 {
			t.Fatalf("%s should stay unpriced: %+v", id, m)
		}
	}
}

func TestExecuteStripsSlugAndPostsChat(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl_1", "object": "chat.completion",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "hi"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer srv.Close()
	restore := provider.SetJSONClientForTests(srv.Client())
	defer restore()

	ex := NewExecutor()
	cred := domain.Credential{
		ID: "src1", Provider: domain.ProviderOpenAI, Label: "OpenRouter",
		Tokens: domain.CredentialTokens{
			AccessToken: "sk-test",
			Extra:       map[string]any{"base_url": srv.URL, "slug": "openrouter"},
		},
	}
	result, err := ex.Execute(context.Background(), cred, map[string]any{
		"model": "openrouter/gpt-4o", "messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, provider.ExecuteOptions{})
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path %s", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth %s", gotAuth)
	}
	if !strings.Contains(gotBody, `"model":"gpt-4o"`) {
		t.Fatalf("body %s", gotBody)
	}
	if strings.Contains(gotBody, "openrouter/gpt-4o") {
		t.Fatalf("slug leaked %s", gotBody)
	}
	gotBody = ""
	result, err = ex.Execute(context.Background(), cred, map[string]any{
		"model": "openrouter/openrouter/auto", "messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, provider.ExecuteOptions{})
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if !strings.Contains(gotBody, `"model":"openrouter/auto"`) {
		t.Fatalf("nested id stripped too far %s", gotBody)
	}
}

func TestModelsForCachesAndPrefixes(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path %s", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"id": "llama-3", "name": "Llama 3"}},
		})
	}))
	defer srv.Close()
	restore := provider.SetJSONClientForTests(srv.Client())
	defer restore()
	ex := NewExecutor()
	cred := domain.Credential{
		ID: "src2", Provider: domain.ProviderOpenAI, Label: "Local",
		Tokens: domain.CredentialTokens{AccessToken: "k", Extra: map[string]any{"base_url": srv.URL, "slug": "local"}},
	}
	first := ex.ModelsFor(context.Background(), cred)
	second := ex.ModelsFor(context.Background(), cred)
	if hits != 1 {
		t.Fatalf("hits %d", hits)
	}
	if len(first) != 1 || first[0].ID != "local/llama-3" || first[0].Name != "Llama 3" {
		t.Fatalf("%+v", first)
	}
	if len(second) != 1 || second[0].ID != first[0].ID {
		t.Fatalf("%+v", second)
	}
}

func TestDiscoverRejectsBadURL(t *testing.T) {
	if _, _, err := Discover(context.Background(), "not-a-url", "sk-test"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDiscoverRejectsEmptyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	restore := provider.SetJSONClientForTests(srv.Client())
	defer restore()
	if _, _, err := Discover(context.Background(), srv.URL, "sk-test"); err == nil {
		t.Fatal("expected empty catalog error")
	}
}

func TestModelsForCacheKeyIncludesSlugAndBase(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"id": fmt.Sprintf("m-%d", hits)}},
		})
	}))
	defer srv.Close()
	restore := provider.SetJSONClientForTests(srv.Client())
	defer restore()
	ex := NewExecutor()
	cred := domain.Credential{
		ID: "src2", Provider: domain.ProviderOpenAI, Label: "Local",
		Tokens: domain.CredentialTokens{AccessToken: "k", Extra: map[string]any{"base_url": srv.URL, "slug": "local"}},
	}
	first := ex.ModelsFor(context.Background(), cred)
	cred.Tokens.Extra["slug"] = "other"
	second := ex.ModelsFor(context.Background(), cred)
	if hits != 2 {
		t.Fatalf("hits %d", hits)
	}
	if len(first) != 1 || first[0].ID != "local/m-1" {
		t.Fatalf("%+v", first)
	}
	if len(second) != 1 || second[0].ID != "other/m-2" {
		t.Fatalf("%+v", second)
	}
}
