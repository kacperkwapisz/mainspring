package grok

import (
	"context"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
)

// count_tokens is answered locally: the context is already cancelled, so any
// upstream call would fail instead of returning a count.
func TestCountTokensDoesNotCallUpstream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := map[string]any{"model": "m", "messages": []any{map[string]any{"role": "user", "content": "hello there"}}}
	res, err := Executor{}.Execute(ctx, domain.Credential{}, body, provider.ExecuteOptions{CountTokens: true})
	if err != nil || !res.OK || res.Status != 200 {
		t.Fatalf("%+v %v", res, err)
	}
	if n, _ := res.Body.(map[string]any)["input_tokens"].(int); n <= 0 {
		t.Fatalf("input_tokens = %v", res.Body)
	}
}
