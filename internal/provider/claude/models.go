package claude

import (
	"context"
	"fmt"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/translate"
)

// Claude's /v1/models answers to a subscription sign-in with the models that
// account can use, each with its context size, output limit and effort levels.
const modelsURL = api + "/v1/models?limit=1000"

var modelCache = provider.NewModelCache(time.Hour)

// ModelsFor returns the models this account offers (see provider.ModelCache).
func (e Executor) ModelsFor(ctx context.Context, credential domain.Credential) []domain.ModelInfo {
	return modelCache.Get(ctx, credential.ID, func(ctx context.Context) ([]domain.ModelInfo, error) {
		res, err := provider.GetJSON(ctx, modelsURL, map[string]string{
			"Authorization":     "Bearer " + credential.Tokens.AccessToken,
			"anthropic-beta":    oauthBeta,
			"anthropic-version": "2023-06-01",
			"Accept":            "application/json",
			"User-Agent":        ua,
		})
		if err != nil {
			return nil, err
		}
		body, _, err := provider.ReadJSON(res)
		if err != nil {
			return nil, err
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, fmt.Errorf("claude models %d", res.StatusCode)
		}
		return provider.WithCatalog(domain.ProviderClaude, ParseModels(body)), nil
	})
}

var effortOrder = []string{"low", "medium", "high", "xhigh", "max"}

// ParseModels reads a /v1/models answer.
func ParseModels(body any) []domain.ModelInfo {
	var out []domain.ModelInfo
	for _, raw := range translate.AsArr(translate.AsMap(body)["data"]) {
		m := translate.AsMap(raw)
		id := translate.AsStr(m["id"])
		if id == "" || translate.AsStr(m["lifecycle"]) == "retired" {
			continue
		}
		caps := translate.AsMap(m["capabilities"])
		supported := func(v any) bool { return translate.AsMap(v)["supported"] == true }
		input := []string{"text"}
		if supported(caps["image_input"]) {
			input = append(input, "image")
		}
		info := domain.ModelInfo{
			ID: id, Object: "model", OwnedBy: string(domain.ProviderClaude),
			Name: translate.AsStr(m["display_name"]), DisplayName: translate.AsStr(m["display_name"]),
			Input: input, InputModalities: input,
			Reasoning: supported(caps["thinking"]),
		}
		if n, ok := m["max_input_tokens"].(float64); ok {
			info.ContextLength = int(n)
		}
		if n, ok := m["max_tokens"].(float64); ok {
			info.MaxOutputTokens = int(n)
		}
		effort := translate.AsMap(caps["effort"])
		if effort["supported"] == true {
			seen := map[string]bool{}
			for _, level := range effortOrder {
				if supported(effort[level]) {
					info.Efforts = append(info.Efforts, level)
					seen[level] = true
				}
			}
			for level, v := range effort {
				if level != "supported" && !seen[level] && supported(v) {
					info.Efforts = append(info.Efforts, level)
				}
			}
		}
		out = append(out, info)
	}
	return out
}
