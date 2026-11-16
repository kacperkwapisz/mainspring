package translate

import (
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

func TestDeveloperMessagesReachClaudeAsSystem(t *testing.T) {
	chat := TranslateRequest(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-5-5", false, map[string]any{
		"messages": []any{
			map[string]any{"role": "developer", "content": "you are pi"},
			map[string]any{"role": "user", "content": "hi"},
		},
	})
	body := AsMap(chat.Body)
	if got := flattenText(body["system"]); got != "you are pi" {
		t.Fatalf("chat system = %q", got)
	}
	for _, m := range AsArr(body["messages"]) {
		if strings.Contains(flattenText(AsMap(m)["content"]), "you are pi") {
			t.Fatalf("developer prompt leaked into messages: %v", body["messages"])
		}
	}

	resp := TranslateRequest(domain.InboundOpenAIResponses, domain.FormatClaude, "claude-opus-5-5", false, map[string]any{
		"instructions": "base",
		"input": []any{
			map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": "you are pi"}}},
			map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hi"}}},
		},
	})
	body = AsMap(resp.Body)
	if got := flattenText(body["system"]); got != "base\n\nyou are pi" {
		t.Fatalf("responses system = %q", got)
	}
	if n := len(AsArr(body["messages"])); n != 1 {
		t.Fatalf("want only the user message, got %v", body["messages"])
	}
}

func TestDeveloperMessagesForCodexAndGrok(t *testing.T) {
	chat := map[string]any{"messages": []any{
		map[string]any{"role": "developer", "content": "you are pi"},
		map[string]any{"role": "user", "content": "hi"},
	}}
	roleOf := func(res RequestResult) string { return AsStr(AsMap(AsArr(AsMap(res.Body)["input"])[0])["role"]) }
	if r := roleOf(TranslateRequest(domain.InboundOpenAIChat, domain.FormatCodex, "gpt-5.5", false, chat)); r != "developer" {
		t.Fatalf("codex chat role = %q", r)
	}
	if r := roleOf(TranslateRequest(domain.InboundOpenAIChat, domain.FormatGrok, "grok-4.5", false, chat)); r != "system" {
		t.Fatalf("grok chat role = %q", r)
	}
	responses := map[string]any{"input": []any{
		map[string]any{"type": "message", "role": "developer", "content": "you are pi"},
		map[string]any{"type": "message", "role": "user", "content": "hi"},
	}}
	if r := roleOf(TranslateRequest(domain.InboundOpenAIResponses, domain.FormatCodex, "gpt-5.5", false, responses)); r != "developer" {
		t.Fatalf("codex responses role = %q", r)
	}
	if r := roleOf(TranslateRequest(domain.InboundOpenAIResponses, domain.FormatGrok, "grok-4.5", false, responses)); r != "system" {
		t.Fatalf("grok responses role = %q", r)
	}
	// The inbound body is not rewritten in place.
	if AsStr(AsMap(AsArr(responses["input"])[0])["role"]) != "developer" {
		t.Fatal("inbound body was changed")
	}
	// Chat-format upstreams (Cursor, OpenAI-compatible sources) get "system".
	cursor := TranslateRequest(domain.InboundOpenAIResponses, domain.FormatCursor, "cursor/auto", false, responses)
	if r := AsStr(AsMap(AsArr(AsMap(cursor.Body)["messages"])[0])["role"]); r != "system" {
		t.Fatalf("cursor role = %q", r)
	}
}

func TestClaudeEffortThinking(t *testing.T) {
	efforts := []string{"low", "medium", "high", "xhigh", "max"}
	chat := func(effort string) map[string]any {
		return map[string]any{"reasoning_effort": effort, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	}
	cases := []struct {
		name    string
		from    domain.InboundFormat
		body    map[string]any
		efforts []string
		want    string // "budget", "adaptive:<effort>", "adaptive", "none", "keep"
	}{
		{"chat high with efforts", domain.InboundOpenAIChat, chat("high"), efforts, "adaptive:high"},
		{"chat minimal maps to low", domain.InboundOpenAIChat, chat("minimal"), efforts, "adaptive:low"},
		{"chat max clamps down", domain.InboundOpenAIChat, chat("max"), []string{"low", "medium", "high"}, "adaptive:high"},
		{"chat low clamps up", domain.InboundOpenAIChat, chat("low"), []string{"high", "max"}, "adaptive:high"},
		{"chat none drops thinking", domain.InboundOpenAIChat, chat("none"), efforts, "none"},
		{"chat without efforts keeps budget", domain.InboundOpenAIChat, chat("high"), nil, "budget"},
		{"chat explicit thinking kept", domain.InboundOpenAIChat, map[string]any{"reasoning_effort": "high", "thinking": map[string]any{"type": "enabled", "budget_tokens": 1024.0}}, efforts, "keep"},
		{"responses high with efforts", domain.InboundOpenAIResponses, map[string]any{"reasoning": map[string]any{"effort": "high"}, "input": "hi"}, efforts, "adaptive:high"},
		{"responses no effort", domain.InboundOpenAIResponses, map[string]any{"reasoning": map[string]any{"summary": "auto"}, "input": "hi"}, efforts, "adaptive"},
		{"responses without efforts keeps budget", domain.InboundOpenAIResponses, map[string]any{"reasoning": map[string]any{"effort": "high"}, "input": "hi"}, nil, "budget"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := AsMap(TranslateRequestWith(c.from, domain.FormatClaude, "claude-opus-5-5", false, c.body, RequestHints{Efforts: c.efforts}).Body)
			th := AsMap(out["thinking"])
			effort := AsStr(AsMap(out["output_config"])["effort"])
			switch {
			case c.want == "budget":
				if th["type"] != "enabled" || th["budget_tokens"] == nil || out["output_config"] != nil {
					t.Fatalf("want budget thinking, got %v %v", out["thinking"], out["output_config"])
				}
			case c.want == "none":
				if out["thinking"] != nil || out["output_config"] != nil {
					t.Fatalf("want no thinking, got %v %v", out["thinking"], out["output_config"])
				}
			case c.want == "keep":
				if th["type"] != "enabled" || th["budget_tokens"] != 1024.0 {
					t.Fatalf("explicit thinking changed: %v", out["thinking"])
				}
			case c.want == "adaptive":
				if th["type"] != "adaptive" || out["output_config"] != nil {
					t.Fatalf("want adaptive without effort, got %v %v", out["thinking"], out["output_config"])
				}
			default:
				if th["type"] != "adaptive" || "adaptive:"+effort != c.want {
					t.Fatalf("want %s, got %v effort %q", c.want, out["thinking"], effort)
				}
			}
		})
	}
}

func TestAnthropicAdaptiveThinkingPassesThrough(t *testing.T) {
	in := map[string]any{
		"thinking":      map[string]any{"type": "adaptive"},
		"output_config": map[string]any{"effort": "xhigh"},
		"messages":      []any{map[string]any{"role": "user", "content": "hi"}},
	}
	out := AsMap(TranslateRequestWith(domain.InboundClaudeMessages, domain.FormatClaude, "claude-opus-5-5", false, in, RequestHints{Efforts: []string{"low", "high"}}).Body)
	if AsMap(out["thinking"])["type"] != "adaptive" || AsMap(out["output_config"])["effort"] != "xhigh" {
		t.Fatalf("changed: %v %v", out["thinking"], out["output_config"])
	}
}

func TestTokenCountAnswersPassThrough(t *testing.T) {
	count := map[string]any{"input_tokens": 42}
	for _, to := range []domain.ExecutorFormat{domain.FormatCodex, domain.FormatGrok, domain.FormatCursor} {
		got := AsMap(TranslateResponse(domain.InboundClaudeMessages, to, "m", nil, count))
		if len(got) != 1 || got["input_tokens"] != 42 {
			t.Fatalf("%s: %v", to, got)
		}
	}
}
