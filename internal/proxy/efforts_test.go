package proxy

import (
	"context"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

// effortExec is a Claude executor with a live model list; it keeps the body
// each request was sent with.
type effortExec struct {
	fakeExec
	live []domain.ModelInfo
	sent *[]map[string]any
}

func (e effortExec) ModelsFor(context.Context, domain.Credential) []domain.ModelInfo { return e.live }
func (e effortExec) Execute(_ context.Context, _ domain.Credential, body any, _ provider.ExecuteOptions) (provider.ExecuteResult, error) {
	*e.sent = append(*e.sent, AsMap(body))
	return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{
		"id": "msg", "content": []any{map[string]any{"type": "text", "text": "ok"}},
		"stop_reason": "end_turn", "usage": map[string]any{"input_tokens": 1.0, "output_tokens": 1.0},
	}}, nil
}

func TestReasoningEffortBecomesAdaptiveThinkingOnClaude(t *testing.T) {
	var sent []map[string]any
	ex := effortExec{
		fakeExec: fakeExec{id: domain.ProviderClaude, format: domain.FormatClaude},
		live: []domain.ModelInfo{
			{ID: "claude-opus-5-5", Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
			{ID: "claude-haiku-4-5"},
		},
		sent: &sent,
	}
	hub, d := testHub(t, map[domain.ProviderID]provider.Executor{domain.ProviderClaude: ex})
	defer d.Close()
	_, _ = hub.Vault.Save(store.SaveCredential{ID: "a", Provider: domain.ProviderClaude, Label: "A", Tokens: domain.CredentialTokens{AccessToken: "a"}})
	created, _ := hub.Keys.Create("t", nil, nil, nil)
	key, _ := hub.Keys.Verify(created.Secret)
	for _, model := range []string{"claude-opus-5-5", "claude-haiku-4-5"} {
		res, err := Proxy(context.Background(), hub, Request{
			Inbound: domain.InboundOpenAIChat, Key: *key,
			Body: map[string]any{"model": model, "reasoning_effort": "high", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		})
		if err != nil || !res.OK {
			t.Fatalf("%s: %+v %v", model, res, err)
		}
	}
	if len(sent) != 2 {
		t.Fatalf("sent %d", len(sent))
	}
	if th := AsMap(sent[0]["thinking"]); th["type"] != "adaptive" || AsMap(sent[0]["output_config"])["effort"] != "high" {
		t.Fatalf("model with efforts: %v %v", sent[0]["thinking"], sent[0]["output_config"])
	}
	if th := AsMap(sent[1]["thinking"]); th["type"] != "enabled" || th["budget_tokens"] == nil || sent[1]["output_config"] != nil {
		t.Fatalf("model without efforts: %v %v", sent[1]["thinking"], sent[1]["output_config"])
	}
}

func TestCountTokensAnswerReachesTheCallerUnchanged(t *testing.T) {
	hub, d := testHub(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderCodex: fakeExec{id: domain.ProviderCodex, format: domain.FormatCodex,
			models: []domain.ModelInfo{{ID: "gpt-5.5"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				return provider.CountTokensEstimate(map[string]any{"input": "hello there"})
			}},
	})
	defer d.Close()
	_, _ = hub.Vault.Save(store.SaveCredential{ID: "a", Provider: domain.ProviderCodex, Label: "A", Tokens: domain.CredentialTokens{AccessToken: "a"}})
	created, _ := hub.Keys.Create("t", nil, nil, nil)
	key, _ := hub.Keys.Verify(created.Secret)
	res, err := Proxy(context.Background(), hub, Request{
		Inbound: domain.InboundClaudeMessages, Key: *key, CountTokens: true,
		Body: map[string]any{"model": "gpt-5.5", "messages": []any{map[string]any{"role": "user", "content": "hello there"}}},
	})
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	body := AsMap(res.Body)
	if len(body) != 1 || body["input_tokens"] != 4 {
		t.Fatalf("want {input_tokens: 4}, got %v", body)
	}
}
