package translate

import "github.com/kacperkwapisz/mainspring/internal/domain"

type requestFn func(model string, stream bool, body any) RequestResult
type responseFn func(model string, original, upstream any) any
type streamFn func(model string, original, chunk any, state *StreamState) []string

var requestFns = map[string]requestFn{
	"openai.chat→claude":      openaiChatToClaude,
	"openai.chat→codex":       openaiChatToCodex,
	"openai.chat→grok":        openaiChatToGrok,
	"claude.messages→claude":  claudeToClaude,
	"claude.messages→codex":   claudeToCodex,
	"claude.messages→grok":    claudeToCodex,
	"openai.responses→claude": responsesToClaude,
	"openai.responses→codex":  responsesToCodex,
	"openai.responses→grok":   responsesToGrokResponses,
	"openai.chat→cursor":      identityChat,
	"claude.messages→cursor":  claudeToGrok,
	"openai.responses→cursor": responsesToGrok,
	"openai.chat→openai":      identityChat,
	"claude.messages→openai":  claudeToChat,
	"openai.responses→openai": responsesToChat,
}

var responseFns = map[string]responseFn{
	"openai.chat→claude":      claudeToOpenaiChat,
	"openai.chat→codex":       codexToOpenaiChat,
	"openai.chat→grok":        codexToOpenaiChat,
	"claude.messages→claude":  claudeFromClaude,
	"claude.messages→codex":   codexToClaude,
	"claude.messages→grok":    codexToClaude,
	"openai.responses→claude": claudeToResponses,
	"openai.responses→codex":  codexToResponses,
	"openai.responses→grok":   codexToResponses,
	"openai.chat→cursor":      grokToOpenaiChat,
	"claude.messages→cursor":  grokToClaude,
	"openai.responses→cursor": grokToResponses,
	"openai.chat→openai":      grokToOpenaiChat,
	"claude.messages→openai":  grokToClaude,
	"openai.responses→openai": grokToResponses,
}

var streamFns = map[string]streamFn{
	"openai.chat→claude":      claudeStreamToOpenaiChat,
	"openai.chat→codex":       codexStreamToOpenaiChat,
	"openai.chat→grok":        codexStreamToOpenaiChat,
	"claude.messages→claude":  claudeStreamIdentity,
	"claude.messages→codex":   codexStreamToClaude,
	"claude.messages→grok":    codexStreamToClaude,
	"openai.responses→claude": claudeStreamToResponses,
	"openai.responses→codex":  codexStreamToResponses,
	"openai.responses→grok":   codexStreamToResponses,
	"openai.chat→cursor":      grokStreamToOpenaiChat,
	"claude.messages→cursor":  grokStreamToClaude,
	"openai.responses→cursor": grokStreamToResponses,
	"openai.chat→openai":      chatStreamToOpenaiChat,
	"claude.messages→openai":  grokStreamToClaude,
	"openai.responses→openai": grokStreamToResponses,
}

func identityChat(model string, stream bool, body any) RequestResult {
	return RequestResult{Model: model, Stream: stream, Body: RewriteModel(body, model)}
}

// claudeToChat and responsesToChat translate to an OpenAI-compatible Chat
// server (a source, or an OpenCode chat model). Anthropic and Responses
// streams always end with the request's usage, but a Chat stream sends it only
// when stream_options.include_usage asks, so asking is part of the
// translation. A Chat client's own request goes through as sent (identityChat).
func claudeToChat(model string, stream bool, body any) RequestResult {
	return withStreamUsage(claudeToGrok(model, stream, body))
}

func responsesToChat(model string, stream bool, body any) RequestResult {
	return withStreamUsage(responsesToGrok(model, stream, body))
}

func withStreamUsage(res RequestResult) RequestResult {
	if !res.Stream {
		return res
	}
	out := cloneMap(AsMap(res.Body))
	opts := cloneMap(AsMap(out["stream_options"]))
	opts["include_usage"] = true
	out["stream_options"] = opts
	res.Body = out
	return res
}

func TranslateRequest(from domain.InboundFormat, to domain.ExecutorFormat, model string, stream bool, body any) RequestResult {
	key := string(from) + "→" + string(to)
	if fn, ok := requestFns[key]; ok {
		return fn(model, stream, body)
	}
	return RequestResult{Model: model, Stream: stream, Body: RewriteModel(body, model)}
}

func TranslateResponse(from domain.InboundFormat, to domain.ExecutorFormat, model string, original, upstream any) any {
	if isTokenCount(upstream) {
		return upstream
	}
	key := string(from) + "→" + string(to)
	if fn, ok := responseFns[key]; ok {
		return fn(model, original, upstream)
	}
	return upstream
}

func TranslateStream(from domain.InboundFormat, to domain.ExecutorFormat, model string, original, chunk any, state *StreamState) []string {
	key := string(from) + "→" + string(to)
	if fn, ok := streamFns[key]; ok {
		return fn(model, original, chunk, state)
	}
	return []string{"data: " + mustJSON(chunk)}
}

func DoneLine(from domain.InboundFormat) string {
	if from == domain.InboundOpenAIChat {
		return "data: [DONE]"
	}
	return ""
}

// isTokenCount reports whether upstream is a count_tokens answer
// ({"input_tokens": N}). Only Claude gives one; it is already in the shape
// the caller asked for and goes back as Claude sent it, without the model a
// message reply gets. Providers without a count endpoint answer with an
// error, which the proxy sends in the caller's error shape.
func isTokenCount(upstream any) bool {
	m, ok := upstream.(map[string]any)
	if !ok || len(m) != 1 {
		return false
	}
	_, ok = m["input_tokens"]
	return ok
}
