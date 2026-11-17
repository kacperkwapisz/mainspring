package claude

import (
	"context"
	"regexp"
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
//
// Claude's search can't reach some sites (reddit.com among them) and refuses a
// request that names them. Those domains are dropped and the search runs
// again. When every allowed domain is refused, the result is retryable, so the
// web tools try another provider.
func (e Executor) WebSearch(ctx context.Context, credential domain.Credential, model, query string, allowed, blocked []string) (provider.WebAnswer, provider.ExecuteResult) {
	return searchAround(allowed, blocked, func(allowed, blocked []string) (provider.WebAnswer, provider.ExecuteResult) {
		return e.webSearchOnce(ctx, credential, model, query, allowed, blocked)
	})
}

// searchAround runs search, dropping domains Claude refuses until it answers.
func searchAround(allowed, blocked []string, search func(allowed, blocked []string) (provider.WebAnswer, provider.ExecuteResult)) (provider.WebAnswer, provider.ExecuteResult) {
	for {
		answer, result := search(allowed, blocked)
		refused := refusedDomains(result)
		if len(refused) == 0 {
			return answer, result
		}
		hadAllowed := len(allowed) > 0
		nextAllowed, droppedA := withoutDomains(allowed, refused)
		nextBlocked, droppedB := withoutDomains(blocked, refused)
		if !droppedA && !droppedB {
			return answer, result
		}
		if hadAllowed && len(nextAllowed) == 0 {
			result.Retryable = true
			result.Message = "Claude's web search can't reach " + strings.Join(refused, ", ")
			return answer, result
		}
		allowed, blocked = nextAllowed, nextBlocked
	}
}

func (e Executor) webSearchOnce(ctx context.Context, credential domain.Credential, model, query string, allowed, blocked []string) (provider.WebAnswer, provider.ExecuteResult) {
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

var refusedDomainsRe = regexp.MustCompile(`domains are not accessible to our user agent: \[([^\]]*)\]`)

// refusedDomains reads the domains in Claude's "not accessible to our user
// agent" refusal.
func refusedDomains(result provider.ExecuteResult) []string {
	if result.OK || result.Status != 400 {
		return nil
	}
	msg := result.Message
	if msg == "" {
		msg = translate.AsStr(translate.AsMap(translate.AsMap(result.Body)["error"])["message"])
	}
	m := refusedDomainsRe.FindStringSubmatch(msg)
	if m == nil {
		return nil
	}
	var out []string
	for _, d := range strings.Split(m[1], ",") {
		if d = strings.Trim(strings.TrimSpace(d), `'"`); d != "" {
			out = append(out, d)
		}
	}
	return out
}

func withoutDomains(list, drop []string) ([]string, bool) {
	var out []string
	dropped := false
	for _, d := range list {
		refused := false
		for _, r := range drop {
			if strings.EqualFold(strings.TrimSpace(d), r) {
				refused = true
			}
		}
		if refused {
			dropped = true
			continue
		}
		out = append(out, d)
	}
	return out, dropped
}
