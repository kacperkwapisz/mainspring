package provider

// WithStreamUsage asks a Chat Completions stream for its usage chunk, which
// the meter needs: without include_usage, OpenAI-style streams report no
// tokens. A value the client set is kept. It returns a new map, so the
// client's own stream_options is left as it was.
func WithStreamUsage(opts any) map[string]any {
	out := map[string]any{}
	if m, ok := opts.(map[string]any); ok {
		for k, v := range m {
			out[k] = v
		}
	}
	if _, set := out["include_usage"]; !set {
		out["include_usage"] = true
	}
	return out
}
