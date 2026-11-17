package grok

import (
	"context"
	"net/http"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
)

type countCalls int

func (c *countCalls) RoundTrip(*http.Request) (*http.Response, error) {
	*c++
	return nil, http.ErrHandlerTimeout
}

// Grok has no count endpoint: count_tokens gets a clear error and Grok is never
// asked.
func TestCountTokensIsRefusedWithoutCallingUpstream(t *testing.T) {
	var calls countCalls
	restore := provider.SetJSONClientForTests(&http.Client{Transport: &calls})
	defer restore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := map[string]any{"model": "m", "messages": []any{map[string]any{"role": "user", "content": "hello there"}}}
	res, err := Executor{}.Execute(ctx, domain.Credential{}, body, provider.ExecuteOptions{CountTokens: true})
	want := "Grok can't count tokens; only Claude models support /v1/messages/count_tokens"
	if err != nil || res.OK || res.Status != 400 || res.Message != want {
		t.Fatalf("%+v %v", res, err)
	}
	if e := res.Body.(map[string]any)["error"].(map[string]any); e["type"] != "invalid_request_error" || e["message"] != want {
		t.Fatalf("body %v", res.Body)
	}
	if calls != 0 {
		t.Fatalf("%d upstream calls", calls)
	}
}
