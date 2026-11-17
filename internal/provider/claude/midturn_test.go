package claude

import (
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/translate"
)

// pi's Claude 5.5 models set the effort per turn with role "system" messages
// that carry output_config, bind thinking blocks, and add tools mid-conversation.
// Claude refuses each of these without the beta the client asked for.
func TestDisguiseKeepsTheClientsBetasAndPerTurnMessages(t *testing.T) {
	asked := "interleaved-thinking-2025-05-14,mid-conversation-output-config-2026-07-01,thinking-binding-controls-2026-08-01,inline-tools-2026-09-15"
	prepared := PrepareUpstream(
		map[string]any{
			"model":  "claude-sonnet-5-5",
			"system": []any{map[string]any{"type": "text", "text": "You are pi."}},
			"messages": []any{
				map[string]any{"role": "user", "content": "hi"},
				map[string]any{"role": "system", "content": []any{
					map[string]any{"type": "text", "text": "Now in plan mode."},
					map[string]any{"type": "tool_addition", "tool": map[string]any{"type": "tool_definition", "definition": map[string]any{"name": "plan"}}},
				}},
				map[string]any{"role": "system", "content": []any{}, "output_config": map[string]any{"effort": "high"}},
			},
			"thinking": map[string]any{"type": "adaptive", "display": "summarized", "block_binding": map[string]any{"prefix_mismatch_behavior": "drop_block"}},
		},
		"sk-ant-oat-test", "cred-1", "",
		map[string]string{"user-agent": "pi/1.5.2", "anthropic-beta": asked},
		true, false, "key-1",
	)
	beta := prepared.Headers["anthropic-beta"]
	for _, want := range strings.Split(asked, ",") {
		if !strings.Contains(beta, want) {
			t.Errorf("dropped the client's beta %s: %s", want, beta)
		}
	}
	var effort, addition bool
	for _, raw := range translate.AsArr(translate.AsMap(prepared.Body)["messages"]) {
		m := translate.AsMap(raw)
		if translate.AsStr(m["role"]) != "system" {
			continue
		}
		if translate.AsStr(translate.AsMap(m["output_config"])["effort"]) == "high" {
			effort = true
		}
		for _, p := range translate.AsArr(m["content"]) {
			if translate.AsStr(translate.AsMap(p)["type"]) == "tool_addition" {
				addition = true
			}
		}
	}
	if !effort || !addition {
		t.Fatalf("per-turn messages lost (effort=%v, tool_addition=%v): %v", effort, addition, translate.AsMap(prepared.Body)["messages"])
	}
}

func TestCountTokensKeepsTheClientsBetas(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{"model": "claude-sonnet-5-5", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		"sk-ant-oat-test", "cred-1", "",
		map[string]string{"user-agent": "pi/1.5.2", "anthropic-beta": "mid-conversation-output-config-2026-07-01"},
		false, true, "key-1",
	)
	if !strings.Contains(prepared.Headers["anthropic-beta"], "mid-conversation-output-config-2026-07-01") {
		t.Fatal(prepared.Headers["anthropic-beta"])
	}
}
