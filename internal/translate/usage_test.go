package translate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

// liveClaudeUsage is a usage object Claude returned through the hub.
func liveClaudeUsage() map[string]any {
	return map[string]any{
		"cache_creation":              map[string]any{"ephemeral_1h_input_tokens": 14292.0, "ephemeral_5m_input_tokens": 0.0},
		"cache_creation_input_tokens": 14292.0, "cache_read_input_tokens": 0.0,
		"input_tokens": 2.0, "output_tokens": 16.0,
	}
}

func TestParseUsageConventions(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
		want Usage
	}{
		{"claude live", liveClaudeUsage(), Usage{Input: 2, Output: 16, CacheWrite: 14292, CacheWrite1h: 14292}},
		{"claude with reads and searches", map[string]any{
			"input_tokens": 10.0, "output_tokens": 5.0, "cache_read_input_tokens": 900.0, "cache_creation_input_tokens": 100.0,
			"server_tool_use": map[string]any{"web_search_requests": 3.0},
		}, Usage{Input: 10, Output: 5, CacheRead: 900, CacheWrite: 100, WebSearches: 3}},
		{"responses", map[string]any{
			"input_tokens": 100000.0, "input_tokens_details": map[string]any{"cached_tokens": 90000.0},
			"output_tokens": 1000.0, "output_tokens_details": map[string]any{"reasoning_tokens": 400.0},
		}, Usage{Input: 10000, Output: 1000, CacheRead: 90000, Reasoning: 400}},
		{"chat", map[string]any{
			"prompt_tokens": 50.0, "completion_tokens": 7.0, "prompt_tokens_details": map[string]any{"cached_tokens": 20.0},
		}, Usage{Input: 30, Output: 7, CacheRead: 20}},
		{"cursor counts reads and writes in prompt", map[string]any{
			"prompt_tokens": 1000.0, "completion_tokens": 7.0,
			"prompt_tokens_details": map[string]any{"cached_tokens": 600.0, "cache_write_tokens": 300.0},
			"routed_model":          "claude-4.5-sonnet",
		}, Usage{Input: 100, Output: 7, CacheRead: 600, CacheWrite: 300, Routed: "claude-4.5-sonnet"}},
		{"never below zero", map[string]any{
			"prompt_tokens": 10.0, "prompt_tokens_details": map[string]any{"cached_tokens": 20.0},
		}, Usage{Input: 0, CacheRead: 20}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseUsage(tc.raw)
			if !ok || got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if _, ok := ParseUsage(nil); ok {
		t.Fatal("nil usage should not parse")
	}
}

func TestClaudeUsageToOpenAIClientsCountsAllInput(t *testing.T) {
	upstream := map[string]any{
		"id": "msg_1", "content": []any{map[string]any{"type": "text", "text": "hi"}}, "stop_reason": "end_turn",
		"usage": map[string]any{"input_tokens": 10.0, "output_tokens": 2.0, "cache_read_input_tokens": 900.0, "cache_creation_input_tokens": 100.0},
	}
	chat := AsMap(AsMap(TranslateResponse(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, upstream))["usage"])
	if numOf(chat["prompt_tokens"]) != 1010 || numOf(chat["total_tokens"]) != 1012 {
		t.Fatalf("chat usage %v", chat)
	}
	details := AsMap(chat["prompt_tokens_details"])
	if numOf(details["cached_tokens"]) != 900 || numOf(details["cache_write_tokens"]) != 100 {
		t.Fatalf("chat details %v", details)
	}
	resp := AsMap(AsMap(TranslateResponse(domain.InboundOpenAIResponses, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, upstream))["usage"])
	if numOf(resp["input_tokens"]) != 1010 || numOf(AsMap(resp["input_tokens_details"])["cached_tokens"]) != 900 {
		t.Fatalf("responses usage %v", resp)
	}
	if numOf(AsMap(resp["input_tokens_details"])["cache_write_tokens"]) != 100 {
		t.Fatalf("responses dropped cache writes %v", resp)
	}
}

func TestOpenAIUsageToClaudeClientsExcludesCache(t *testing.T) {
	codex := map[string]any{
		"id": "resp_1", "output": []any{},
		"usage": map[string]any{"input_tokens": 100000.0, "input_tokens_details": map[string]any{"cached_tokens": 90000.0}, "output_tokens": 1000.0},
	}
	got := AsMap(AsMap(TranslateResponse(domain.InboundClaudeMessages, domain.FormatCodex, "gpt-5.5", map[string]any{}, codex))["usage"])
	if numOf(got["input_tokens"]) != 10000 || numOf(got["cache_read_input_tokens"]) != 90000 || numOf(got["output_tokens"]) != 1000 {
		t.Fatalf("codex→claude usage %v", got)
	}
	cursor := map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "hi"}, "finish_reason": "stop"}},
		"usage": map[string]any{"prompt_tokens": 1000.0, "completion_tokens": 7.0,
			"prompt_tokens_details": map[string]any{"cached_tokens": 600.0, "cache_write_tokens": 300.0}},
	}
	got = AsMap(AsMap(TranslateResponse(domain.InboundClaudeMessages, domain.FormatCursor, "m", map[string]any{}, cursor))["usage"])
	if numOf(got["input_tokens"]) != 100 || numOf(got["cache_read_input_tokens"]) != 600 || numOf(got["cache_creation_input_tokens"]) != 300 {
		t.Fatalf("cursor→claude usage %v", got)
	}
}

func TestCodexStreamToClaudeExcludesCache(t *testing.T) {
	state := EmptyStreamState()
	run := func(ev map[string]any) []string {
		return TranslateStream(domain.InboundClaudeMessages, domain.FormatCodex, "gpt-5.5", map[string]any{}, ev, &state)
	}
	run(map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_1"}})
	lines := run(map[string]any{"type": "response.completed", "response": map[string]any{
		"usage": map[string]any{"input_tokens": 100.0, "input_tokens_details": map[string]any{"cached_tokens": 60.0}, "output_tokens": 5.0},
	}})
	delta := sseData(t, lines, "message_delta")
	u := AsMap(delta["usage"])
	if numOf(u["input_tokens"]) != 40 || numOf(u["cache_read_input_tokens"]) != 60 {
		t.Fatalf("message_delta usage %v", u)
	}
	if got := state.Usage(); got.Input != 40 || got.CacheRead != 60 || got.Output != 5 {
		t.Fatalf("state %+v", got)
	}
}

func claudeStreamEvents() []map[string]any {
	return []map[string]any{
		{"type": "message_start", "message": map[string]any{"id": "msg_1", "usage": map[string]any{
			"input_tokens": 2.0, "output_tokens": 1.0, "cache_read_input_tokens": 1000.0, "cache_creation_input_tokens": 14292.0,
			"cache_creation": map[string]any{"ephemeral_1h_input_tokens": 14292.0, "ephemeral_5m_input_tokens": 0.0},
		}}},
		{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": "hi"}},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{
			"output_tokens": 16.0, "server_tool_use": map[string]any{"web_search_requests": 1.0},
		}},
		{"type": "message_stop"},
	}
}

func TestClaudeStreamToOpenAIClientsKeepsCache(t *testing.T) {
	want := Usage{Input: 2, Output: 16, CacheRead: 1000, CacheWrite: 14292, CacheWrite1h: 14292, WebSearches: 1}
	for _, inbound := range []domain.InboundFormat{domain.InboundOpenAIChat, domain.InboundOpenAIResponses, domain.InboundClaudeMessages} {
		t.Run(string(inbound), func(t *testing.T) {
			state := EmptyStreamState()
			var lines []string
			for _, ev := range claudeStreamEvents() {
				lines = append(lines, TranslateStream(inbound, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, ev, &state)...)
			}
			if got := state.Usage(); got != want {
				t.Fatalf("state %+v, want %+v", got, want)
			}
			switch inbound {
			case domain.InboundOpenAIChat:
				var final map[string]any
				for _, l := range lines {
					if strings.Contains(l, `"finish_reason":"stop"`) {
						_ = json.Unmarshal([]byte(strings.TrimPrefix(l, "data: ")), &final)
					}
				}
				u := AsMap(final["usage"])
				if numOf(u["prompt_tokens"]) != 15294 || numOf(AsMap(u["prompt_tokens_details"])["cached_tokens"]) != 1000 {
					t.Fatalf("final chunk usage %v", u)
				}
			case domain.InboundOpenAIResponses:
				done := sseData(t, lines, "response.completed")
				u := AsMap(AsMap(done["response"])["usage"])
				if numOf(u["input_tokens"]) != 15294 || numOf(AsMap(u["input_tokens_details"])["cached_tokens"]) != 1000 {
					t.Fatalf("completed usage %v", u)
				}
			}
		})
	}
}

func TestChatPassthroughForwardsTheUpstreamsUsageChunk(t *testing.T) {
	content := map[string]any{"id": "c1", "choices": []any{map[string]any{"index": 0.0, "delta": map[string]any{"content": "hi"}}}}
	finish := map[string]any{"id": "c1", "choices": []any{map[string]any{"index": 0.0, "delta": map[string]any{}, "finish_reason": "stop"}}}
	usage := map[string]any{"id": "c1", "choices": []any{}, "usage": map[string]any{"prompt_tokens": 30.0, "completion_tokens": 4.0}}

	// The request went upstream as sent, so a usage chunk the upstream sends
	// is the client's, asked for or not, and it is metered either way.
	state := EmptyStreamState()
	plain := map[string]any{"model": "local/m"}
	TranslateStream(domain.InboundOpenAIChat, domain.FormatOpenAI, "local/m", plain, content, &state)
	lines := TranslateStream(domain.InboundOpenAIChat, domain.FormatOpenAI, "local/m", plain, usage, &state)
	if len(lines) != 1 || !strings.Contains(lines[0], `"prompt_tokens":30`) {
		t.Fatalf("usage chunk dropped: %v", lines)
	}
	if lines := TranslateStream(domain.InboundOpenAIChat, domain.FormatOpenAI, "local/m", plain, finish, &state); !contains(lines, "data: [DONE]") {
		t.Fatalf("finish %v", lines)
	}
	if state.PromptTokens != 30 || state.CompletionTokens != 4 {
		t.Fatalf("usage not metered %+v", state)
	}

	// A client that asked gets [DONE] after the usage chunk that follows the finish.
	state = EmptyStreamState()
	asked := map[string]any{"model": "local/m", "stream_options": map[string]any{"include_usage": true}}
	lines = TranslateStream(domain.InboundOpenAIChat, domain.FormatOpenAI, "local/m", asked, finish, &state)
	if contains(lines, "data: [DONE]") {
		t.Fatalf("[DONE] before the usage chunk: %v", lines)
	}
	lines = TranslateStream(domain.InboundOpenAIChat, domain.FormatOpenAI, "local/m", asked, usage, &state)
	if len(lines) != 2 || !strings.Contains(lines[0], `"prompt_tokens":30`) || lines[1] != "data: [DONE]" {
		t.Fatalf("usage chunk then [DONE], got %v", lines)
	}
}

func TestChatStreamToClaudeWaitsForTrailingUsage(t *testing.T) {
	finish := map[string]any{"id": "c1", "choices": []any{map[string]any{"index": 0.0, "delta": map[string]any{"content": "hi"}, "finish_reason": "stop"}}}
	usage := map[string]any{"id": "c1", "choices": []any{}, "usage": map[string]any{
		"prompt_tokens": 30.0, "completion_tokens": 4.0, "prompt_tokens_details": map[string]any{"cached_tokens": 20.0},
	}}
	state := EmptyStreamState()
	lines := TranslateStream(domain.InboundClaudeMessages, domain.FormatOpenAI, "local/m", map[string]any{}, finish, &state)
	if containsSub(lines, "message_delta") {
		t.Fatalf("message_delta sent before usage: %v", lines)
	}
	lines = TranslateStream(domain.InboundClaudeMessages, domain.FormatOpenAI, "local/m", map[string]any{}, usage, &state)
	u := AsMap(sseData(t, lines, "message_delta")["usage"])
	if numOf(u["input_tokens"]) != 10 || numOf(u["cache_read_input_tokens"]) != 20 || !containsSub(lines, "message_stop") {
		t.Fatalf("closing events %v", lines)
	}
	if FlushStream(&state) != nil {
		t.Fatal("closing events sent twice")
	}

	// No usage at all: the closing events come out when the stream ends.
	state = EmptyStreamState()
	TranslateStream(domain.InboundOpenAIResponses, domain.FormatOpenAI, "local/m", map[string]any{}, finish, &state)
	if !containsSub(FlushStream(&state), "response.completed") || !state.Finished {
		t.Fatalf("flush %+v", state)
	}
}

func TestCodexIncompleteAndFailedKeepUsage(t *testing.T) {
	usage := map[string]any{"input_tokens": 100.0, "input_tokens_details": map[string]any{"cached_tokens": 60.0}, "output_tokens": 5.0}
	for _, inbound := range []domain.InboundFormat{domain.InboundOpenAIChat, domain.InboundOpenAIResponses, domain.InboundClaudeMessages} {
		t.Run(string(inbound)+"/incomplete", func(t *testing.T) {
			state := EmptyStreamState()
			TranslateStream(inbound, domain.FormatCodex, "gpt-5.5", map[string]any{}, map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}}, &state)
			TranslateStream(inbound, domain.FormatCodex, "gpt-5.5", map[string]any{}, map[string]any{
				"type": "response.incomplete", "response": map[string]any{"status": "incomplete", "usage": usage},
			}, &state)
			if got := state.Usage(); got.Input != 40 || got.CacheRead != 60 || got.Output != 5 || !state.Finished {
				t.Fatalf("state %+v finished=%v", got, state.Finished)
			}
		})
		t.Run(string(inbound)+"/failed", func(t *testing.T) {
			state := EmptyStreamState()
			TranslateStream(inbound, domain.FormatCodex, "gpt-5.5", map[string]any{}, map[string]any{
				"type": "response.failed", "response": map[string]any{"status": "failed", "usage": usage, "error": map[string]any{"message": "boom"}},
			}, &state)
			if got := state.Usage(); got.Input != 40 || got.Output != 5 || state.Finished || state.Error != "boom" {
				t.Fatalf("state %+v finished=%v error=%q", got, state.Finished, state.Error)
			}
		})
	}
}

// sseData finds the event named name among SSE lines and decodes its data.
func sseData(t *testing.T, lines []string, name string) map[string]any {
	t.Helper()
	for _, l := range lines {
		if !strings.HasPrefix(l, "event: "+name+"\n") {
			continue
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(l[strings.Index(l, "data: ")+6:]), &out)
		return out
	}
	t.Fatalf("no %s event in %v", name, lines)
	return nil
}
