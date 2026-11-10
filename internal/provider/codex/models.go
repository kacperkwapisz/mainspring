package codex

import (
	"context"
	"fmt"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/translate"
)

// Codex reads its model picker from this endpoint. Models marked
// visibility "hide" are internal (review, reserve) and aren't offered.
const modelsURL = "https://chatgpt.com/backend-api/codex/models?client_version=" + ClientVersion

var modelCache = provider.NewModelCache(time.Hour)

// ModelsFor returns the models this account offers (see provider.ModelCache).
func (e Executor) ModelsFor(ctx context.Context, credential domain.Credential) []domain.ModelInfo {
	return modelCache.Get(ctx, credential.ID, func(ctx context.Context) ([]domain.ModelInfo, error) {
		headers := map[string]string{
			"Authorization": "Bearer " + credential.Tokens.AccessToken,
			"Accept":        "application/json",
			"Originator":    Originator,
			"User-Agent":    UserAgent,
		}
		if id := accountID(credential); id != "" {
			headers["ChatGPT-Account-Id"] = id
		}
		res, err := provider.GetJSON(ctx, modelsURL, headers)
		if err != nil {
			return nil, err
		}
		body, _, err := provider.ReadJSON(res)
		if err != nil {
			return nil, err
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, fmt.Errorf("codex models %d", res.StatusCode)
		}
		return provider.WithCatalog(domain.ProviderCodex, ParseModels(body)), nil
	})
}

// ParseModels reads a /codex/models answer.
func ParseModels(body any) []domain.ModelInfo {
	var out []domain.ModelInfo
	for _, raw := range translate.AsArr(translate.AsMap(body)["models"]) {
		m := translate.AsMap(raw)
		id := translate.AsStr(m["slug"])
		if id == "" || translate.AsStr(m["visibility"]) == "hide" {
			continue
		}
		input := []string{}
		for _, v := range translate.AsArr(m["input_modalities"]) {
			if s := translate.AsStr(v); s != "" {
				input = append(input, s)
			}
		}
		if len(input) == 0 {
			input = []string{"text"}
		}
		info := domain.ModelInfo{
			ID: id, Object: "model", OwnedBy: string(domain.ProviderCodex),
			Name: translate.AsStr(m["display_name"]), DisplayName: translate.AsStr(m["display_name"]),
			Input: input, InputModalities: input,
		}
		if n, ok := m["context_window"].(float64); ok {
			info.ContextLength = int(n)
		}
		for _, lvl := range translate.AsArr(m["supported_reasoning_levels"]) {
			if s := translate.AsStr(translate.AsMap(lvl)["effort"]); s != "" {
				info.Efforts = append(info.Efforts, s)
			}
		}
		info.Reasoning = len(info.Efforts) > 0
		out = append(out, info)
	}
	return out
}
