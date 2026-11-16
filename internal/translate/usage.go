package translate

// Usage is one response's token counts in Anthropic's convention: Input is
// the uncached input only, and cache reads and writes are counted on their
// own. The meter stores this shape for every provider, and each client format
// is rendered from it.
type Usage struct {
	Input        int64 // uncached input tokens
	Output       int64
	CacheRead    int64
	CacheWrite   int64 // all cache writes, CacheWrite1h included
	CacheWrite1h int64 // the part of CacheWrite kept for Claude's one-hour TTL
	Reasoning    int64
	WebSearches  int64 // Claude's server-side web searches, billed per search
	Routed       string
}

// AllInput is every input token of the request, cached or not.
func (u Usage) AllInput() int64 { return u.Input + u.CacheRead + u.CacheWrite }

// ParseUsage reads a provider's usage object in whichever convention it uses.
// OpenAI-style counts (Chat's prompt_tokens, or Responses' input_tokens with
// input_tokens_details) include the cached tokens; Anthropic's input_tokens
// sits next to cache_read_input_tokens and cache_creation_input_tokens and
// excludes them. ok is false when there's no usage at all.
func ParseUsage(raw any) (u Usage, ok bool) {
	m := AsMap(raw)
	if len(m) == 0 {
		return Usage{}, false
	}
	u.Output = intOf(m["completion_tokens"], m["output_tokens"])
	u.Reasoning = intOf(AsMap(firstNonNil(m["completion_tokens_details"], m["output_tokens_details"]))["reasoning_tokens"])
	u.Routed = AsStr(m["routed_model"])
	details := AsMap(firstNonNil(m["prompt_tokens_details"], m["input_tokens_details"]))
	_, chat := AsNum(m["prompt_tokens"])
	if !chat && isClaudeUsage(m) {
		u.Input = intOf(m["input_tokens"])
		u.CacheRead = intOf(m["cache_read_input_tokens"])
		u.CacheWrite = intOf(m["cache_creation_input_tokens"])
		u.CacheWrite1h = min(intOf(AsMap(m["cache_creation"])["ephemeral_1h_input_tokens"]), u.CacheWrite)
		u.WebSearches = intOf(AsMap(m["server_tool_use"])["web_search_requests"])
		return u, true
	}
	all := intOf(m["prompt_tokens"], m["input_tokens"])
	u.CacheRead = intOf(details["cached_tokens"], m["cache_read_input_tokens"])
	u.CacheWrite = intOf(details["cache_write_tokens"], m["cache_creation_input_tokens"])
	u.Input = max(all-u.CacheRead-u.CacheWrite, 0)
	return u, true
}

// isClaudeUsage tells Anthropic's usage object from a Responses one: only
// Anthropic reports the cache in top-level cache_* fields.
func isClaudeUsage(m map[string]any) bool {
	for _, k := range []string{"cache_read_input_tokens", "cache_creation_input_tokens", "cache_creation", "server_tool_use"} {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

// mergeClaudeUsage folds one Claude stream event's usage into the state.
// message_start carries the input side and message_delta the running output
// count (newer API versions repeat the input fields there too), so each field
// present keeps its latest value.
func mergeClaudeUsage(state *StreamState, raw any) {
	m := AsMap(raw)
	if state == nil || len(m) == 0 {
		return
	}
	state.UsageSeen = true
	set := func(dst *int64, v any) {
		if n, ok := AsNum(v); ok {
			*dst = int64(n)
		}
	}
	set(&state.PromptTokens, m["input_tokens"])
	set(&state.CompletionTokens, m["output_tokens"])
	set(&state.CacheRead, m["cache_read_input_tokens"])
	set(&state.CacheWrite, m["cache_creation_input_tokens"])
	set(&state.CacheWrite1h, AsMap(m["cache_creation"])["ephemeral_1h_input_tokens"])
	set(&state.WebSearches, AsMap(m["server_tool_use"])["web_search_requests"])
}

// setStreamUsage replaces the state's usage with a complete usage object, as
// OpenAI-style streams send it once near the end.
func setStreamUsage(state *StreamState, raw any) {
	if state == nil {
		return
	}
	u, ok := ParseUsage(raw)
	if !ok {
		return
	}
	state.UsageSeen = true
	state.PromptTokens, state.CompletionTokens = u.Input, u.Output
	state.CacheRead, state.CacheWrite, state.CacheWrite1h = u.CacheRead, u.CacheWrite, u.CacheWrite1h
	state.ReasoningTokens, state.WebSearches = u.Reasoning, u.WebSearches
	if u.Routed != "" {
		state.RoutedModel = u.Routed
	}
}

// Usage returns what the stream has reported so far.
func (s *StreamState) Usage() Usage {
	if s == nil {
		return Usage{}
	}
	return Usage{
		Input: s.PromptTokens, Output: s.CompletionTokens,
		CacheRead: s.CacheRead, CacheWrite: s.CacheWrite, CacheWrite1h: min(s.CacheWrite1h, s.CacheWrite),
		Reasoning: s.ReasoningTokens, WebSearches: s.WebSearches, Routed: s.RoutedModel,
	}
}

// chatUsage renders usage for OpenAI Chat clients: prompt_tokens counts every
// input token and cached_tokens says how many of them were read from cache.
func chatUsage(u Usage) map[string]any {
	prompt := u.AllInput()
	out := map[string]any{"prompt_tokens": prompt, "completion_tokens": u.Output, "total_tokens": prompt + u.Output}
	if u.CacheRead != 0 || u.CacheWrite != 0 {
		details := map[string]any{"cached_tokens": u.CacheRead}
		if u.CacheWrite != 0 {
			details["cache_write_tokens"] = u.CacheWrite
		}
		out["prompt_tokens_details"] = details
	}
	if u.Reasoning != 0 {
		out["completion_tokens_details"] = map[string]any{"reasoning_tokens": u.Reasoning}
	}
	return out
}

// responsesUsage renders usage for OpenAI Responses clients, with the same
// inclusive input count as Chat.
func responsesUsage(u Usage) map[string]any {
	input := u.AllInput()
	out := map[string]any{"input_tokens": input, "output_tokens": u.Output, "total_tokens": input + u.Output}
	if u.CacheRead != 0 || u.CacheWrite != 0 {
		details := map[string]any{"cached_tokens": u.CacheRead}
		if u.CacheWrite != 0 {
			details["cache_write_tokens"] = u.CacheWrite
		}
		out["input_tokens_details"] = details
	}
	if u.Reasoning != 0 {
		out["output_tokens_details"] = map[string]any{"reasoning_tokens": u.Reasoning}
	}
	return out
}

// claudeUsage renders usage for Anthropic Messages clients: input_tokens is
// the uncached input only.
func claudeUsage(u Usage) map[string]any {
	out := map[string]any{"input_tokens": u.Input, "output_tokens": u.Output}
	if u.CacheRead != 0 {
		out["cache_read_input_tokens"] = u.CacheRead
	}
	if u.CacheWrite != 0 {
		out["cache_creation_input_tokens"] = u.CacheWrite
	}
	return out
}

func intOf(vals ...any) int64 {
	for _, v := range vals {
		if n, ok := AsNum(v); ok {
			return int64(n)
		}
	}
	return 0
}

// FlushStream returns the closing events a stream held back waiting for its
// usage. Call it once the upstream stream has ended.
func FlushStream(state *StreamState) []string {
	if state == nil || state.pending == nil {
		return nil
	}
	lines := state.pending()
	state.pending = nil
	return lines
}

// releasePending emits held-back closing events once the usage has arrived.
func releasePending(state *StreamState) []string {
	if state == nil || state.pending == nil || !state.UsageSeen {
		return nil
	}
	return FlushStream(state)
}
