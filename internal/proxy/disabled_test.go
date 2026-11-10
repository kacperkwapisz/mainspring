package proxy

import (
	"context"
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

func TestTurnedOffModelIsHiddenAndRefused(t *testing.T) {
	called := 0
	hub, d := testHub(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderClaude: fakeExec{
			id: domain.ProviderClaude, format: domain.FormatClaude,
			models: []domain.ModelInfo{{ID: "claude-opus-4-7", Object: "model"}, {ID: "claude-sonnet-5", Object: "model"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				called++
				return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{"id": "m", "content": []any{}, "usage": map[string]any{}}}
			},
		},
	})
	defer d.Close()
	if _, err := hub.Vault.Save(store.SaveCredential{Provider: domain.ProviderClaude, Label: "a@example.com", Tokens: domain.CredentialTokens{AccessToken: "t"}}); err != nil {
		t.Fatal(err)
	}
	created, _ := hub.Keys.Create("t", nil, nil, nil)
	key, _ := hub.Keys.Verify(created.Secret)

	if err := SetModelEnabled(hub, "claude-opus-4-7", false); err != nil {
		t.Fatal(err)
	}
	for _, m := range ServedModels(hub) {
		if m.ID == "claude-opus-4-7" {
			t.Fatal("turned-off model still listed")
		}
	}
	if len(ListModels(hub)) < 2 {
		t.Fatal("the panel's full list must keep it, so it can be turned back on")
	}
	for _, asked := range []string{"claude-opus-4-7", "claude-opus-4-7[1m]", "claude-opus-4-7-20260101"} {
		res, err := Proxy(context.Background(), hub, Request{Inbound: domain.InboundClaudeMessages, Body: map[string]any{"model": asked, "max_tokens": 5.0, "messages": []any{}}, Key: *key})
		msg := AsStr(AsMap(AsMap(res.Body)["error"])["message"])
		if err != nil || res.Status != 404 || !strings.Contains(msg, "turned off") {
			t.Fatalf("%s: status %d %q %v", asked, res.Status, msg, err)
		}
	}
	if called != 0 {
		t.Fatal("a turned-off model reached the provider")
	}
	if res, _ := Proxy(context.Background(), hub, Request{Inbound: domain.InboundClaudeMessages, Body: map[string]any{"model": "claude-sonnet-5", "max_tokens": 5.0, "messages": []any{}}, Key: *key}); !res.OK {
		t.Fatalf("other models must keep working: %d", res.Status)
	}
	_ = SetModelEnabled(hub, "claude-opus-4-7", true)
	if len(DisabledModels(hub)) != 0 {
		t.Fatal("turning it back on didn't clear it")
	}
}
