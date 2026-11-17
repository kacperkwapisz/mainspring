package translate

import "testing"

func countBreakpoints(o any) int {
	n := 0
	switch v := o.(type) {
	case map[string]any:
		if _, ok := v["cache_control"]; ok {
			n++
		}
		for k, x := range v {
			if k != "cache_control" {
				n += countBreakpoints(x)
			}
		}
	case []any:
		for _, x := range v {
			n += countBreakpoints(x)
		}
	}
	return n
}

// Claude Code marks four breakpoints and leaves its tools unmarked. A fifth
// one added here made Claude refuse the request.
func TestClaudeRequestsKeepTheClientsBreakpoints(t *testing.T) {
	cc := map[string]any{"type": "ephemeral"}
	body := map[string]any{
		"model": "claude-opus-5-5",
		"system": []any{
			map[string]any{"type": "text", "text": "billing"},
			map[string]any{"type": "text", "text": "A", "cache_control": cc},
			map[string]any{"type": "text", "text": "B", "cache_control": cc},
		},
		"tools": []any{map[string]any{"name": "Read", "input_schema": map[string]any{"type": "object"}}},
		"messages": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "hi", "cache_control": cc}}},
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "yo"}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "go", "cache_control": cc}}},
		},
	}
	out := TranslateRequest("claude.messages", "claude", "claude-opus-5-5", true, body)
	if n := countBreakpoints(out.Body); n != 4 {
		t.Fatalf("breakpoints %d, want the client's 4", n)
	}
	plain := map[string]any{"model": "claude-opus-5-5", "system": "sys", "tools": []any{map[string]any{"name": "t"}},
		"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	if n := countBreakpoints(TranslateRequest("claude.messages", "claude", "claude-opus-5-5", false, plain).Body); n != 0 {
		t.Fatalf("added %d breakpoints to a request that had none", n)
	}
}
