package provider

import "encoding/json"

// tokenCountFields are the request parts that make up a prompt in the formats
// Mainspring sends upstream (Anthropic, OpenAI chat and Responses).
var tokenCountFields = []string{"system", "instructions", "messages", "input", "tools"}

// EstimateTokens roughly counts a request's input tokens: the JSON size of its
// prompt parts over four, rounded up. Providers without a count endpoint use it.
func EstimateTokens(body any) int {
	rec, _ := body.(map[string]any)
	size := 0
	for _, k := range tokenCountFields {
		v, ok := rec[k]
		if !ok || v == nil {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		size += len(raw)
	}
	return (size + 3) / 4
}

// CountTokensEstimate answers a count_tokens call without asking the provider,
// so counting never runs (and bills) a real generation.
func CountTokensEstimate(body any) ExecuteResult {
	return ExecuteResult{OK: true, Status: 200, Body: map[string]any{"input_tokens": EstimateTokens(body)}}
}
