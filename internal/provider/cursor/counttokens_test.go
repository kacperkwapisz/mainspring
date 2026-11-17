package cursor

import (
	"context"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
)

// Cursor has no count endpoint: count_tokens gets a clear error and Cursor is never
// asked. The context is already cancelled, so a call upstream would fail with
// a different error instead.
func TestCountTokensIsRefusedWithoutCallingUpstream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := map[string]any{"model": "m", "messages": []any{map[string]any{"role": "user", "content": "hello there"}}}
	res, err := (&Executor{}).Execute(ctx, domain.Credential{}, body, provider.ExecuteOptions{CountTokens: true})
	want := "Cursor can't count tokens; only Claude models support /v1/messages/count_tokens"
	if err != nil || res.OK || res.Status != 400 || res.Message != want {
		t.Fatalf("%+v %v", res, err)
	}
	if e := res.Body.(map[string]any)["error"].(map[string]any); e["type"] != "invalid_request_error" || e["message"] != want {
		t.Fatalf("body %v", res.Body)
	}
}
