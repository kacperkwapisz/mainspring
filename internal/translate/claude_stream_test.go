package translate

import (
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

func TestClaudePassthroughStreamRecordsUsageAndFinish(t *testing.T) {
	state := EmptyStreamState()
	for _, ev := range []map[string]any{
		{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 12.0, "cache_read_input_tokens": 180000.0, "cache_creation_input_tokens": 900.0, "output_tokens": 1.0}}},
		{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": "hi"}},
		{"type": "message_delta", "usage": map[string]any{"output_tokens": 420.0}},
		{"type": "message_stop"},
	} {
		TranslateStream(domain.InboundClaudeMessages, domain.FormatClaude, "claude-opus-5-5", nil, ev, &state)
	}
	if !state.Finished || state.Error != "" {
		t.Fatalf("finished %v error %q", state.Finished, state.Error)
	}
	if state.PromptTokens != 12 || state.CompletionTokens != 420 || state.CacheRead != 180000 || state.CacheWrite != 900 {
		t.Fatalf("usage %+v", state)
	}
}
