package provider

import "testing"

func TestCountTokensUnsupported(t *testing.T) {
	res := CountTokensUnsupported("Codex")
	want := "Codex can't count tokens; only Claude models support /v1/messages/count_tokens"
	if res.OK || res.Status != 400 || res.Retryable || res.Message != want {
		t.Fatalf("%+v", res)
	}
	e, _ := res.Body.(map[string]any)["error"].(map[string]any)
	if e["type"] != "invalid_request_error" || e["message"] != want {
		t.Fatalf("body %v", res.Body)
	}
}
