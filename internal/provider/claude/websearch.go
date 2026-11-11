package claude

import (
	"context"
	"strings"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/translate"
)

// Claude Code's WebSearch tool runs as its own small request: this system
// prompt, "Perform a web search for the query: …", and the web_search server
// tool. Mainspring sends the same request, shaped like Claude Code's own.
const webSearchSystem = "You run a web search for another Claude model, which is waiting on the result and can search again. Search once, then reply with a brief summary of the results."

// WebSearch searches the web on this account.
func (e Executor) WebSearch(ctx context.Context, credential domain.Credential, model, query string, allowed, blocked []string) (provider.WebAnswer, provider.ExecuteResult) {
	tool := map[string]any{"type": "web_search_20250305", "name": "web_search", "max_uses": 8.0}
	if len(allowed) > 0 {
		tool["allowed_domains"] = toAny(allowed)
	}
	if len(blocked) > 0 {
		tool["blocked_domains"] = toAny(blocked)
	}
	body := nativeBody(model, webSearchSystem, "Perform a web search for the query: "+query, 4096)
	body["tools"] = []any{tool}
	result := e.executeNative(ctx, credential, body)
	return answerFrom(result.Body), result
}

// Ask sends one question with a system prompt on this account and returns the
// reply, for questions about a fetched page.
func (e Executor) Ask(ctx context.Context, credential domain.Credential, model, system, prompt string) (provider.WebAnswer, provider.ExecuteResult) {
	result := e.executeNative(ctx, credential, nativeBody(model, system, prompt, 4096))
	return answerFrom(result.Body), result
}

func nativeBody(model, system, prompt string, maxTokens int) map[string]any {
	return map[string]any{
		"model": model, "max_tokens": float64(maxTokens), "stream": false,
		"system": []any{
			map[string]any{"type": "text", "text": cliIdentity},
			map[string]any{"type": "text", "text": system},
		},
		"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": prompt}}}},
		"metadata": map[string]any{"user_id": "{}"},
	}
}

// executeNative sends a request Mainspring built in Claude Code's own shape, so
// it isn't disguised again, with the beta list Claude Code would send for it.
func (e Executor) executeNative(ctx context.Context, credential domain.Credential, body map[string]any) provider.ExecuteResult {
	inbound := map[string]string{"User-Agent": ua, "x-app": "cli", "anthropic-beta": codeBeta}
	prepared := PrepareUpstream(body, credential.Tokens.AccessToken, credential.ID, credential.Tokens.AccountID, inbound, false, false, "")
	prepared.Headers["anthropic-beta"] = claudeBetas(translate.AsMap(prepared.Body), "", false)
	res, err := provider.PostJSON(ctx, api+"/v1/messages?beta=true", prepared.Body, prepared.Headers)
	if err != nil {
		return provider.FailFromErr(err)
	}
	result, err := wrap(res, false, nil)
	if err != nil {
		return provider.FailFromErr(err)
	}
	return result
}

// answerFrom reads the reply text and every page the search returned or cited.
func answerFrom(body any) provider.WebAnswer {
	var a provider.WebAnswer
	var text strings.Builder
	for _, raw := range translate.AsArr(translate.AsMap(body)["content"]) {
		block := translate.AsMap(raw)
		switch translate.AsStr(block["type"]) {
		case "text":
			text.WriteString(translate.AsStr(block["text"]))
			for _, c := range translate.AsArr(block["citations"]) {
				cm := translate.AsMap(c)
				a.AddSource(translate.AsStr(cm["title"]), translate.AsStr(cm["url"]))
			}
		case "web_search_tool_result":
			for _, r := range translate.AsArr(block["content"]) {
				rm := translate.AsMap(r)
				a.AddSource(translate.AsStr(rm["title"]), translate.AsStr(rm["url"]))
			}
		}
	}
	a.Text = strings.TrimSpace(text.String())
	return a
}

func toAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}
