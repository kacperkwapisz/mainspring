package claude

import (
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/translate"
)

func TestUnconfirmedCallersGetCLIBetas(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model":    "claude-opus-4-7",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			"tools":    []any{map[string]any{"name": "lookup", "input_schema": map[string]any{"type": "object"}}},
		},
		"sk-ant-oat-test", "cred-1", "",
		map[string]string{"user-agent": "Cursor/1.0"},
		false, false, "key-1",
	)
	if prepared.Headers["User-Agent"] != ua {
		t.Fatalf("ua %s", prepared.Headers["User-Agent"])
	}
	beta := prepared.Headers["anthropic-beta"]
	for _, want := range []string{"claude-code-20250219", "oauth-2025-04-20", "advanced-tool-use-2025-11-20"} {
		if !strings.Contains(beta, want) {
			t.Fatalf("missing %s in %s", want, beta)
		}
	}
	body := translate.AsMap(prepared.Body)
	system := translate.AsArr(body["system"])
	if !strings.HasPrefix(translate.AsStr(translate.AsMap(system[0])["text"]), "x-anthropic-billing-header:") {
		t.Fatalf("%v", system[0])
	}
	if !strings.Contains(translate.AsStr(translate.AsMap(system[0])["text"]), "cch=") {
		t.Fatal("cch")
	}
	if translate.AsStr(translate.AsMap(system[1])["text"]) != cliIdentity {
		t.Fatalf("%v", system[1])
	}
}

func TestOmitsRedactWhenThinkingDisplay(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model":    "claude-opus-4-7",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			"thinking": map[string]any{"type": "enabled", "display": "summarized"},
		},
		"sk-ant-oat-test", "cred-1", "", map[string]string{}, false, false, "key-1",
	)
	if strings.Contains(prepared.Headers["anthropic-beta"], "redact-thinking-2026-02-12") {
		t.Fatal(prepared.Headers["anthropic-beta"])
	}
}

func TestForcedToolChoiceDropsThinking(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model":       "claude-opus-4-7",
			"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
			"thinking":    map[string]any{"type": "enabled"},
			"tool_choice": map[string]any{"type": "any"},
		},
		"sk-ant-oat-test", "cred-1", "", map[string]string{}, false, false, "key-1",
	)
	if _, ok := translate.AsMap(prepared.Body)["thinking"]; ok {
		t.Fatal("thinking still present")
	}
}

func TestNativeClaudeCodeKeepsItsSDKFingerprint(t *testing.T) {
	inbound := map[string]string{
		"User-Agent": "claude-cli/2.1.322 (external, cli)", "x-app": "cli",
		"anthropic-beta":              "claude-code-20250219,oauth-2025-04-20",
		"X-Stainless-Package-Version": "0.128.0", "X-Stainless-Runtime-Version": "v26.12.1",
	}
	body := map[string]any{"model": "claude-opus-5", "metadata": map[string]any{"user_id": "{\"session_id\":\"s\"}"},
		"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	p := PrepareUpstream(body, "tok", "cred", "", inbound, false, false, "k")
	if p.Headers["User-Agent"] != "claude-cli/2.1.322 (external, cli)" || p.Headers["X-Stainless-Package-Version"] != "0.128.0" || p.Headers["X-Stainless-Runtime-Version"] != "v26.12.1" {
		t.Fatalf("native fingerprint not kept: %v", p.Headers)
	}
	if p.Headers["anthropic-beta"] != "claude-code-20250219,oauth-2025-04-20" {
		t.Fatalf("native betas changed: %s", p.Headers["anthropic-beta"])
	}
}

func TestCloakedSystemPromptSendsTheBetaItNeeds(t *testing.T) {
	body := map[string]any{"model": "claude-sonnet-5", "system": "Answer in French.",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	p := PrepareUpstream(body, "tok", "cred", "", map[string]string{"User-Agent": "curl/8"}, false, false, "k")
	if !hasRoleSystem(p.Body.(map[string]any)) {
		t.Fatal("the disguise should move the system prompt into a role system message")
	}
	if !strings.Contains(p.Headers["anthropic-beta"], "mid-conversation-system-2026-04-07") {
		t.Fatalf("role system messages need the beta: %s", p.Headers["anthropic-beta"])
	}
}

func TestModelsThatRefuseMidSystemKeepTheSystemField(t *testing.T) {
	refusal := map[string]any{"error": map[string]any{"message": "role 'system' is not supported on this model"}}
	if !RefusesMidSystem(refusal) {
		t.Fatal("refusal not recognised")
	}
	noMidSystem.Store("claude-old-1", true)
	defer noMidSystem.Delete("claude-old-1")
	body := map[string]any{"model": "claude-old-1", "system": "Answer in French.",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	p := PrepareUpstream(body, "tok", "cred", "", map[string]string{"User-Agent": "curl/8"}, false, false, "k")
	got := p.Body.(map[string]any)
	if hasRoleSystem(got) {
		t.Fatal("must not send role system messages to this model")
	}
	sys := translate.AsArr(got["system"])
	if translate.AsStr(translate.AsMap(sys[len(sys)-1])["text"]) != "Answer in French." {
		t.Fatalf("system prompt lost: %v", sys)
	}
}
