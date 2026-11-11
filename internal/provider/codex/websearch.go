package codex

import (
	"context"
	"strings"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/translate"
)

// Codex searches with its native web_search tool in a Responses request.

// WebSearch searches the web on this account.
func (e Executor) WebSearch(ctx context.Context, credential domain.Credential, model, query string, allowed, blocked []string) (provider.WebAnswer, provider.ExecuteResult) {
	instructions := "Search the web once, then reply with a brief summary of the results."
	if len(allowed) > 0 {
		instructions += " Only use results from these domains: " + strings.Join(allowed, ", ") + "."
	}
	if len(blocked) > 0 {
		instructions += " Leave out results from these domains: " + strings.Join(blocked, ", ") + "."
	}
	body := responsesBody(model, instructions, "Perform a web search for the query: "+query)
	body["tools"] = []any{map[string]any{"type": "web_search"}}
	result, err := e.Execute(ctx, credential, body, provider.ExecuteOptions{})
	if err != nil {
		result = provider.FailFromErr(err)
	}
	return answerFrom(result.Body), result
}

// Ask sends one question with instructions on this account and returns the reply.
func (e Executor) Ask(ctx context.Context, credential domain.Credential, model, system, prompt string) (provider.WebAnswer, provider.ExecuteResult) {
	result, err := e.Execute(ctx, credential, responsesBody(model, system, prompt), provider.ExecuteOptions{})
	if err != nil {
		result = provider.FailFromErr(err)
	}
	return answerFrom(result.Body), result
}

func responsesBody(model, instructions, prompt string) map[string]any {
	return map[string]any{
		"model": model, "stream": true, "store": false, "instructions": instructions,
		"input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": prompt}}}},
	}
}

// answerFrom reads the reply text and the pages it cites.
func answerFrom(body any) provider.WebAnswer {
	var a provider.WebAnswer
	var text strings.Builder
	for _, raw := range translate.AsArr(translate.AsMap(body)["output"]) {
		item := translate.AsMap(raw)
		if translate.AsStr(item["type"]) != "message" {
			continue
		}
		for _, c := range translate.AsArr(item["content"]) {
			cm := translate.AsMap(c)
			text.WriteString(translate.AsStr(cm["text"]))
			for _, an := range translate.AsArr(cm["annotations"]) {
				am := translate.AsMap(an)
				if translate.AsStr(am["type"]) == "url_citation" {
					a.AddSource(translate.AsStr(am["title"]), translate.AsStr(am["url"]))
				}
			}
		}
	}
	a.Text = strings.TrimSpace(text.String())
	return a
}
