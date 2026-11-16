package cursor

import "testing"

func TestParseMessagesKeepsADeveloperSystemPrompt(t *testing.T) {
	parsed := ParseMessages([]OpenAIMessage{
		{Role: "developer", Content: "you are pi"},
		{Role: "user", Content: "hi"},
	})
	if parsed.SystemPrompt != "you are pi" {
		t.Fatalf("system prompt = %q", parsed.SystemPrompt)
	}
	if len(parsed.Turns) != 0 || parsed.UserText != "hi" {
		t.Fatalf("%+v", parsed)
	}
}
