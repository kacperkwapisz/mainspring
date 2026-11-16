package proxy

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

// meterHub serves one provider from a fake executor and returns a key for it.
func meterHub(t *testing.T, id domain.ProviderID, format domain.ExecutorFormat, model string, fn func() provider.ExecuteResult) (*Hub, domain.LocalKey) {
	t.Helper()
	hub, d := testHub(t, map[domain.ProviderID]provider.Executor{
		id: fakeExec{
			id: id, format: format,
			models: []domain.ModelInfo{{ID: model, Object: "model", OwnedBy: string(id)}},
			fn:     func(domain.Credential) provider.ExecuteResult { return fn() },
		},
	})
	t.Cleanup(func() { d.Close() })
	if _, err := hub.Vault.Save(store.SaveCredential{ID: "acct", Provider: id, Label: "A", Tokens: domain.CredentialTokens{AccessToken: "t", Extra: map[string]any{}}}); err != nil {
		t.Fatal(err)
	}
	created, err := hub.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := hub.Keys.Verify(created.Secret)
	return hub, *key
}

func streamOf(events ...map[string]any) <-chan any {
	ch := make(chan any, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch
}

func runProxy(t *testing.T, hub *Hub, req Request) Result {
	t.Helper()
	res, err := Proxy(context.Background(), hub, req)
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	if res.Stream != nil {
		for range res.Stream {
		}
	}
	return res
}

func meterToday(t *testing.T, hub *Hub) store.UsageTotals {
	t.Helper()
	today, err := hub.Usage.Since(24 * 60 * 60 * 1000)
	if err != nil {
		t.Fatal(err)
	}
	return today
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func codexBody() map[string]any {
	return map[string]any{
		"id": "resp_1", "object": "response", "status": "completed", "output": []any{},
		"usage": map[string]any{"input_tokens": 100000.0, "input_tokens_details": map[string]any{"cached_tokens": 90000.0}, "output_tokens": 1000.0},
	}
}

// gpt-5.5 lists $5 input, $0.50 cache read and $30 output per million:
// 10k uncached + 90k cached + 1k out is $0.05 + $0.045 + $0.03.
func TestCodexCachedTokensChargedOnce(t *testing.T) {
	for _, inbound := range []domain.InboundFormat{domain.InboundOpenAIChat, domain.InboundOpenAIResponses, domain.InboundClaudeMessages} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", inbound, stream), func(t *testing.T) {
				hub, key := meterHub(t, domain.ProviderCodex, domain.FormatCodex, "gpt-5.5", func() provider.ExecuteResult {
					if stream {
						return provider.ExecuteResult{OK: true, Status: 200, Stream: streamOf(
							map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_1"}},
							map[string]any{"type": "response.completed", "response": codexBody()},
						)}
					}
					return provider.ExecuteResult{OK: true, Status: 200, Body: codexBody()}
				})
				res := runProxy(t, hub, Request{Inbound: inbound, Key: key, Stream: stream, Body: map[string]any{"model": "gpt-5.5", "input": "hi", "messages": []any{}}})
				got := meterToday(t, hub)
				if got.PromptTokens != 10000 || got.CacheReadTokens != 90000 || got.CompletionTokens != 1000 || !near(got.USD, 0.125) {
					t.Fatalf("meter %+v", got)
				}
				if inbound == domain.InboundClaudeMessages && !stream {
					u := AsMap(AsMap(res.Body)["usage"])
					if u["input_tokens"] != int64(10000) || u["cache_read_input_tokens"] != int64(90000) {
						t.Fatalf("client usage %v", u)
					}
				}
			})
		}
	}
}

func claudeLiveBody(usage map[string]any) map[string]any {
	return map[string]any{
		"id": "msg_1", "type": "message", "content": []any{map[string]any{"type": "text", "text": "ok"}},
		"stop_reason": "end_turn", "usage": usage,
	}
}

// claude-opus-4-7 lists $5 input and $25 output per million; one-hour cache
// writes cost twice the input price.
func TestClaudeOneHourCacheWritesPricedAtTwiceInput(t *testing.T) {
	usage := map[string]any{
		"cache_creation":              map[string]any{"ephemeral_1h_input_tokens": 14292.0, "ephemeral_5m_input_tokens": 0.0},
		"cache_creation_input_tokens": 14292.0, "cache_read_input_tokens": 0.0, "input_tokens": 2.0, "output_tokens": 16.0,
	}
	want := (2*5 + 14292*10 + 16*25) / 1e6
	for _, inbound := range []domain.InboundFormat{domain.InboundClaudeMessages, domain.InboundOpenAIChat, domain.InboundOpenAIResponses} {
		t.Run(string(inbound), func(t *testing.T) {
			hub, key := meterHub(t, domain.ProviderClaude, domain.FormatClaude, "claude-opus-4-7", func() provider.ExecuteResult {
				return provider.ExecuteResult{OK: true, Status: 200, Body: claudeLiveBody(usage)}
			})
			runProxy(t, hub, Request{Inbound: inbound, Key: key, Body: map[string]any{"model": "claude-opus-4-7", "messages": []any{}}})
			got := meterToday(t, hub)
			if got.PromptTokens != 2 || got.CacheWriteTokens != 14292 || got.CompletionTokens != 16 || !near(got.USD, want) {
				t.Fatalf("meter %+v, want usd %v", got, want)
			}
		})
	}
}

func TestClaudeStreamToOpenAIClientsMetersCache(t *testing.T) {
	events := func() <-chan any {
		return streamOf(
			map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_1", "usage": map[string]any{
				"input_tokens": 2.0, "output_tokens": 1.0, "cache_read_input_tokens": 1000.0, "cache_creation_input_tokens": 14292.0,
				"cache_creation": map[string]any{"ephemeral_1h_input_tokens": 14292.0},
			}}},
			map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": "hi"}},
			map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 16.0}},
			map[string]any{"type": "message_stop"},
		)
	}
	want := (2*5 + 1000*0.5 + 14292*10 + 16*25) / 1e6
	for _, inbound := range []domain.InboundFormat{domain.InboundOpenAIChat, domain.InboundOpenAIResponses} {
		t.Run(string(inbound), func(t *testing.T) {
			hub, key := meterHub(t, domain.ProviderClaude, domain.FormatClaude, "claude-opus-4-7", func() provider.ExecuteResult {
				return provider.ExecuteResult{OK: true, Status: 200, Stream: events()}
			})
			runProxy(t, hub, Request{Inbound: inbound, Key: key, Stream: true, Body: map[string]any{"model": "claude-opus-4-7", "messages": []any{}}})
			got := meterToday(t, hub)
			if got.PromptTokens != 2 || got.CacheReadTokens != 1000 || got.CacheWriteTokens != 14292 || got.CompletionTokens != 16 || !near(got.USD, want) {
				t.Fatalf("meter %+v, want usd %v", got, want)
			}
		})
	}
}

func TestCodexIncompleteStreamIsMetered(t *testing.T) {
	hub, key := meterHub(t, domain.ProviderCodex, domain.FormatCodex, "gpt-5.5", func() provider.ExecuteResult {
		body := codexBody()
		body["status"] = "incomplete"
		return provider.ExecuteResult{OK: true, Status: 200, Stream: streamOf(
			map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_1"}},
			map[string]any{"type": "response.incomplete", "response": body},
		)}
	})
	runProxy(t, hub, Request{Inbound: domain.InboundOpenAIResponses, Key: key, Stream: true, Body: map[string]any{"model": "gpt-5.5", "input": "hi"}})
	got := meterToday(t, hub)
	if got.PromptTokens != 10000 || got.CacheReadTokens != 90000 || got.CompletionTokens != 1000 || got.Errors != 0 {
		t.Fatalf("meter %+v", got)
	}
}

func TestCountTokensIsNotMetered(t *testing.T) {
	hub, key := meterHub(t, domain.ProviderClaude, domain.FormatClaude, "claude-opus-4-7", func() provider.ExecuteResult {
		return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{"input_tokens": 42.0}}
	})
	runProxy(t, hub, Request{Inbound: domain.InboundClaudeMessages, Key: key, CountTokens: true, Body: map[string]any{"model": "claude-opus-4-7", "messages": []any{}}})
	if got := meterToday(t, hub); got.Requests != 0 {
		t.Fatalf("count_tokens recorded: %+v", got)
	}
}

func TestClaudeWebSearchFeeIsMetered(t *testing.T) {
	usage := map[string]any{"input_tokens": 1000.0, "output_tokens": 100.0, "server_tool_use": map[string]any{"web_search_requests": 3.0}}
	hub, key := meterHub(t, domain.ProviderClaude, domain.FormatClaude, "claude-opus-4-7", func() provider.ExecuteResult {
		return provider.ExecuteResult{OK: true, Status: 200, Body: claudeLiveBody(usage)}
	})
	runProxy(t, hub, Request{Inbound: domain.InboundClaudeMessages, Key: key, Body: map[string]any{"model": "claude-opus-4-7", "messages": []any{}}})
	want := (1000*5+100*25)/1e6 + 3*0.01
	if got := meterToday(t, hub); !near(got.USD, want) {
		t.Fatalf("usd %v, want %v", got.USD, want)
	}
}

func TestOpenAISourceStreamMetersTrailingUsage(t *testing.T) {
	hub, d := testHub(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderOpenAI: fakeExec{id: domain.ProviderOpenAI, format: domain.FormatOpenAI, fn: func(domain.Credential) provider.ExecuteResult {
			return provider.ExecuteResult{OK: true, Status: 200, Stream: streamOf(
				map[string]any{"id": "c1", "choices": []any{map[string]any{"index": 0.0, "delta": map[string]any{"content": "hi"}, "finish_reason": "stop"}}},
				map[string]any{"id": "c1", "choices": []any{}, "usage": map[string]any{"prompt_tokens": 30.0, "completion_tokens": 4.0, "prompt_tokens_details": map[string]any{"cached_tokens": 20.0}}},
			)}
		}},
	})
	defer d.Close()
	_, _ = hub.Vault.Save(store.SaveCredential{ID: "src", Provider: domain.ProviderOpenAI, Label: "Local", Tokens: domain.CredentialTokens{AccessToken: "t", Extra: map[string]any{"base_url": "http://127.0.0.1:1", "slug": "local"}}})
	created, _ := hub.Keys.Create("t", nil, nil, nil)
	key, _ := hub.Keys.Verify(created.Secret)
	res, err := Proxy(context.Background(), hub, Request{Inbound: domain.InboundClaudeMessages, Key: *key, Stream: true, Body: map[string]any{"model": "local/m", "messages": []any{}}})
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	var sawDelta bool
	for line := range res.Stream {
		if len(line) > 20 && line[:20] == "event: message_delta" {
			sawDelta = true
		}
	}
	if !sawDelta {
		t.Fatal("client never got message_delta")
	}
	got := meterToday(t, hub)
	if got.PromptTokens != 10 || got.CacheReadTokens != 20 || got.CompletionTokens != 4 || got.Errors != 0 {
		t.Fatalf("meter %+v", got)
	}
}
