package proxy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

func TestResponsesSayWhichAccountServed(t *testing.T) {
	hub, d := testHub(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderClaude: fakeExec{
			id: domain.ProviderClaude, format: domain.FormatClaude,
			models: []domain.ModelInfo{{ID: "claude-sonnet-5-5"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{"id": "m", "content": []any{}, "usage": map[string]any{}}}
			},
		},
	})
	defer d.Close()
	c, _ := hub.Vault.Save(store.SaveCredential{Provider: domain.ProviderClaude, Label: "alex@example.com", Tokens: domain.CredentialTokens{AccessToken: "t"}})
	used, reset := 38.0, time.Now().Add(2*time.Hour).UnixMilli()
	hub.Router.ObserveWindows(c.ID, "headers", []route.Window{{ID: "claude-5h", Label: "5 hours", UsedPercent: &used, ResetsAt: &reset}})
	created, _ := hub.Keys.Create("t", nil, nil, nil)
	key, _ := hub.Keys.Verify(created.Secret)
	req := Request{Inbound: domain.InboundClaudeMessages, Key: *key, Body: map[string]any{"model": "claude-sonnet-5-5", "max_tokens": 5.0, "messages": []any{}}}

	res, err := Proxy(context.Background(), hub, req)
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	if res.Header.Get("X-Mainspring-Account") != "alex@example.com" || res.Header.Get("X-Mainspring-Provider") != "claude" {
		t.Fatalf("headers %v", res.Header)
	}
	var limits []limitHeader
	if err := json.Unmarshal([]byte(res.Header.Get("X-Mainspring-Limits")), &limits); err != nil || len(limits) != 1 || limits[0].Left != 62 || limits[0].ResetsAt != reset {
		t.Fatalf("limits %q %v", res.Header.Get("X-Mainspring-Limits"), err)
	}

	_ = hub.Settings.Set(SettingSharePool, "0")
	res, _ = Proxy(context.Background(), hub, req)
	if res.Header.Get("X-Mainspring-Account") != "" || res.Header.Get("X-Mainspring-Limits") != "" {
		t.Fatalf("sharing is off but headers went out: %v", res.Header)
	}
}

func TestHeadersSayWhyASessionMoved(t *testing.T) {
	hub, d := testHub(t, nil)
	defer d.Close()
	from, _ := hub.Vault.Save(store.SaveCredential{Provider: domain.ProviderClaude, Label: "alex@example.com", Tokens: domain.CredentialTokens{AccessToken: "t"}})
	to, _ := hub.Vault.Save(store.SaveCredential{Provider: domain.ProviderClaude, Label: "sam@example.com", Tokens: domain.CredentialTokens{AccessToken: "t"}})
	h := accountHeaders(hub, nil, domain.ProviderClaude, to, from.ID, "5-hour limit hit")
	if h.Get("X-Mainspring-Moved-From") != "alex@example.com" || h.Get("X-Mainspring-Moved-Reason") != "5-hour limit hit" {
		t.Fatalf("%v", h)
	}
}
