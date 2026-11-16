package domain

import "testing"

func TestCursorClaudeID(t *testing.T) {
	for id, want := range map[string]string{
		"claude-4-sonnet":                   "claude-sonnet-4",
		"claude-4.5-opus-high-thinking":     "claude-opus-4-5",
		"claude-4.6-sonnet-medium-thinking": "claude-sonnet-4-6",
		"claude-4.6-opus-max-fast":          "claude-opus-4-6",
		"claude-opus-4-7-high":              "",
		"claude-fable-5-high":               "",
		"gpt-5.5":                           "",
	} {
		if got := CursorClaudeID(id); got != want {
			t.Errorf("%s: got %q want %q", id, got, want)
		}
	}
}
