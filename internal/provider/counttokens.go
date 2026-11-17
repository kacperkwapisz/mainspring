package provider

// CountTokensUnsupported answers a count_tokens call for a provider that has
// no count endpoint (name is its display name, such as "Codex"). Only
// Anthropic counts a prompt's tokens, so the caller gets a clear error instead
// of a guess, and the provider is never asked.
func CountTokensUnsupported(name string) ExecuteResult {
	msg := name + " can't count tokens; only Claude models support /v1/messages/count_tokens"
	return ExecuteResult{
		OK: false, Status: 400, Retryable: false, Message: msg,
		Body: map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": msg}},
	}
}
