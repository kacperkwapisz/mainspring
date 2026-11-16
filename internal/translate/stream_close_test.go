package translate

import (
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

func countSub(lines []string, want string) int {
	n := 0
	for _, l := range lines {
		n += strings.Count(l, want)
	}
	return n
}

func TestNothingFollowsDone(t *testing.T) {
	asked := map[string]any{"stream_options": map[string]any{"include_usage": true}}
	// Usage arrives on a chunk before the finish, so [DONE] goes out with
	// the finish; a usage-only chunk after it must not.
	early := map[string]any{"id": "c1", "choices": []any{map[string]any{"index": 0.0, "delta": map[string]any{"content": "hi"}}}, "usage": map[string]any{"prompt_tokens": 3.0, "completion_tokens": 1.0}}
	finish := map[string]any{"id": "c1", "choices": []any{map[string]any{"index": 0.0, "delta": map[string]any{}, "finish_reason": "stop"}}}
	late := map[string]any{"id": "c1", "choices": []any{}, "usage": map[string]any{"prompt_tokens": 3.0, "completion_tokens": 2.0}}
	state := EmptyStreamState()
	var all []string
	for _, c := range []map[string]any{early, finish, late} {
		all = append(all, TranslateStream(domain.InboundOpenAIChat, domain.FormatOpenAI, "m", asked, c, &state)...)
	}
	if all[len(all)-1] != "data: [DONE]" || countSub(all, "[DONE]") != 1 {
		t.Fatalf("[DONE] must be last and sent once: %v", all)
	}
	if state.CompletionTokens != 2 {
		t.Fatalf("late usage still metered: %+v", state)
	}
}

func TestClosingEventsAreSentOnce(t *testing.T) {
	finish := map[string]any{"id": "c1", "choices": []any{map[string]any{"index": 0.0, "delta": map[string]any{"content": "hi"}, "finish_reason": "stop"}}}
	again := map[string]any{"id": "c1", "choices": []any{map[string]any{"index": 0.0, "delta": map[string]any{}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 3.0, "completion_tokens": 1.0}}
	for inbound, closing := range map[domain.InboundFormat]string{
		domain.InboundClaudeMessages:  "event: message_stop",
		domain.InboundOpenAIResponses: "event: response.completed",
	} {
		state := EmptyStreamState()
		var all []string
		for _, c := range []map[string]any{finish, again} {
			all = append(all, TranslateStream(inbound, domain.FormatOpenAI, "m", map[string]any{}, c, &state)...)
		}
		all = append(all, FlushStream(&state)...)
		if n := countSub(all, closing); n != 1 {
			t.Errorf("%s: %d closing events, want 1: %v", inbound, n, all)
		}
	}
}

func TestCodexFailureClosesTheClientStream(t *testing.T) {
	created := map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}}
	failed := map[string]any{"type": "response.failed", "response": map[string]any{"status": "failed", "error": map[string]any{"message": "boom"}}}

	state := EmptyStreamState()
	TranslateStream(domain.InboundClaudeMessages, domain.FormatCodex, "gpt-5.5", map[string]any{}, created, &state)
	lines := TranslateStream(domain.InboundClaudeMessages, domain.FormatCodex, "gpt-5.5", map[string]any{}, failed, &state)
	ev := sseData(t, lines, "error")
	if ev["type"] != "error" || AsMap(ev["error"])["type"] != "api_error" || AsMap(ev["error"])["message"] != "boom" {
		t.Fatalf("claude error event %v", ev)
	}

	state = EmptyStreamState()
	TranslateStream(domain.InboundOpenAIChat, domain.FormatCodex, "gpt-5.5", map[string]any{}, created, &state)
	lines = TranslateStream(domain.InboundOpenAIChat, domain.FormatCodex, "gpt-5.5", map[string]any{}, failed, &state)
	if len(lines) != 2 || !strings.Contains(lines[0], `"finish_reason":"error"`) || !strings.Contains(lines[0], `"content":"boom"`) || lines[1] != "data: [DONE]" {
		t.Fatalf("chat error chunk then [DONE], got %v", lines)
	}
	if state.Finished || state.Error != "boom" {
		t.Fatalf("a failed stream is not finished: %+v", state)
	}
}

func TestChatClientThatTurnedUsageOffGetsNoUsageChunk(t *testing.T) {
	off := map[string]any{"stream_options": map[string]any{"include_usage": false}}
	finish := map[string]any{"id": "c1", "choices": []any{map[string]any{"index": 0.0, "delta": map[string]any{}, "finish_reason": "stop"}}}
	usage := map[string]any{"id": "c1", "choices": []any{}, "usage": map[string]any{"prompt_tokens": 30.0, "completion_tokens": 4.0}}
	state := EmptyStreamState()
	lines := TranslateStream(domain.InboundOpenAIChat, domain.FormatOpenAI, "m", off, finish, &state)
	lines = append(lines, TranslateStream(domain.InboundOpenAIChat, domain.FormatOpenAI, "m", off, usage, &state)...)
	if lines[len(lines)-1] != "data: [DONE]" || containsSub(lines, `"choices":[]`) {
		t.Fatalf("usage chunk reached a client that turned it off: %v", lines)
	}
	if state.PromptTokens != 30 {
		t.Fatalf("usage not metered: %+v", state)
	}
}
