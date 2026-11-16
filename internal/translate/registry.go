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
	"openai.chat→openai":      chatToOpenAI,
	"claude.messages→openai":  claudeToGrok,
	"openai.responses→openai": responsesToGrok,
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
	"openai.chat→openai":      grokStreamToOpenaiChat,
	"claude.messages→openai":  grokStreamToClaude,
	"openai.responses→openai": grokStreamToResponses,
}

func identityChat(model string, stream bool, body any) RequestResult {
	return RequestResult{Model: model, Stream: stream, Body: RewriteModel(body, model)}
}

// chatToOpenAI passes a Chat request to an OpenAI-compatible server (a
// source, or an OpenCode chat model) with "developer" messages sent as
// "system": every compatible server takes system, some reject developer, and
// OpenAI reads system as developer for reasoning models.
func chatToOpenAI(model string, stream bool, body any) RequestResult {
	res := identityChat(model, stream, body)
	msgs, ok := AsMap(res.Body)["messages"].([]any)
	if !ok {
		return res
	}
	next := make([]any, len(msgs))
	changed := false
	for i, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok || AsStr(msg["role"]) != "developer" {
			next[i] = m
			continue
		}
		c := cloneMap(msg)
		c["role"] = "system"
		next[i], changed = c, true
	}
	if !changed {
		return res
	}
	out := cloneMap(AsMap(res.Body)) // the body can be the client's own map
	out["messages"] = next
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
// ({"input_tokens": N}), which is already in the Anthropic shape the caller
// asked for and must not be turned into a message.
func isTokenCount(upstream any) bool {
	m, ok := upstream.(map[string]any)
	if !ok || len(m) != 1 {
		return false
	}
	_, ok = m["input_tokens"]
	return ok
}
