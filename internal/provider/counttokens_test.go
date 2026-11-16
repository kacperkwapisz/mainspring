package provider

import "testing"

func TestEstimateTokens(t *testing.T) {
	body := map[string]any{
		"model":    "gpt-5.5",
		"system":   "abcd",                                                 // "\"abcd\"" = 6 bytes
		"messages": []any{map[string]any{"role": "user", "content": "hi"}}, // 31 bytes
	}
	// (6 + 31) / 4, rounded up; model and other fields don't count.
	if n := EstimateTokens(body); n != 10 {
		t.Fatalf("got %d", n)
	}
	if n := EstimateTokens(nil); n != 0 {
		t.Fatalf("empty body: %d", n)
	}
	res := CountTokensEstimate(body)
	if !res.OK || res.Status != 200 || res.Body.(map[string]any)["input_tokens"] != 10 {
		t.Fatalf("%+v", res)
	}
}
