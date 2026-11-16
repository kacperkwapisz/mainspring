package provider

// WithStreamUsage asks a Chat Completions stream for its usage chunk, which
// the meter needs: without include_usage, OpenAI-style streams report no
// tokens. It is always on upstream, even when the client set it to false;
// the translator forwards the usage chunk only to clients that asked for it.
// It returns a new map, so the client's own stream_options is left as it was.
func WithStreamUsage(opts any) map[string]any {
	out := map[string]any{}
	if m, ok := opts.(map[string]any); ok {
		for k, v := range m {
			out[k] = v
		}
	}
	out["include_usage"] = true
	return out
}
