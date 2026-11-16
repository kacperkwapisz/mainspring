package translate

import "github.com/kacperkwapisz/mainspring/internal/domain"

// RequestHints carries what the proxy knows about the model a request goes to.
type RequestHints struct {
	// Efforts are the effort levels a Claude model takes, from its /v1/models
	// entry. With them, an OpenAI reasoning effort becomes adaptive thinking
	// plus output_config.effort, which is what current Claude models expect.
	// Without them the request keeps budget thinking.
	Efforts []string
}

// TranslateRequestWith is TranslateRequest plus what the proxy knows about the
// target model.
func TranslateRequestWith(from domain.InboundFormat, to domain.ExecutorFormat, model string, stream bool, body any, hints RequestHints) RequestResult {
	res := TranslateRequest(from, to, model, stream, body)
	if to == domain.FormatClaude && len(hints.Efforts) > 0 {
		applyClaudeEffort(from, AsMap(body), AsMap(res.Body), hints.Efforts)
	}
	return res
}

// applyClaudeEffort swaps the budget thinking an OpenAI-format request was
// given for adaptive thinking at the requested effort. Anthropic-format
// requests and explicit "thinking" objects are left as the client sent them.
func applyClaudeEffort(from domain.InboundFormat, in, out map[string]any, efforts []string) {
	var effort string
	switch from {
	case domain.InboundOpenAIChat:
		if in["thinking"] != nil {
			return
		}
		effort = AsStr(in["reasoning_effort"])
		if effort == "" {
			return
		}
	case domain.InboundOpenAIResponses:
		if in["reasoning"] == nil {
			return
		}
		effort = AsStr(AsMap(in["reasoning"])["effort"])
	default:
		return
	}
	if effort == "none" {
		delete(out, "thinking")
		return
	}
	out["thinking"] = map[string]any{"type": "adaptive"}
	if level := clampEffort(effort, efforts); level != "" {
		oc := cloneMap(AsMap(out["output_config"]))
		oc["effort"] = level
		out["output_config"] = oc
	}
}

var effortRank = map[string]int{"minimal": 0, "low": 0, "medium": 1, "high": 2, "xhigh": 3, "max": 4}

// clampEffort picks the supported effort closest to the one asked for: the
// highest level at or below it, else the lowest above it. "minimal" counts as
// "low". An effort it doesn't know returns "", leaving the model's default.
func clampEffort(effort string, supported []string) string {
	want, ok := effortRank[effort]
	if !ok {
		return ""
	}
	below, above := "", ""
	for _, s := range supported {
		r, ok := effortRank[s]
		if !ok || s == "minimal" {
			continue
		}
		if r <= want && (below == "" || r > effortRank[below]) {
			below = s
		}
		if r > want && (above == "" || r < effortRank[above]) {
			above = s
		}
	}
	if below != "" {
		return below
	}
	return above
}
