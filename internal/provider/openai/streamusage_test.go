package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
)

func TestStreamAlwaysAsksForUsage(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	restore := provider.SetJSONClientForTests(srv.Client())
	defer restore()
	ex := NewExecutor()
	cred := domain.Credential{
		ID: "src1", Provider: domain.ProviderOpenAI,
		Tokens: domain.CredentialTokens{AccessToken: "k", Extra: map[string]any{"base_url": srv.URL, "slug": "local"}},
	}
	includeUsage := func() any {
		return got["stream_options"].(map[string]any)["include_usage"]
	}

	client := map[string]any{"model": "local/m", "messages": []any{}}
	res, err := ex.Execute(context.Background(), cred, client, provider.ExecuteOptions{Stream: true})
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	for range res.Stream {
	}
	if includeUsage() != true {
		t.Fatalf("stream_options %v", got["stream_options"])
	}
	if _, touched := client["stream_options"]; touched || client["model"] != "local/m" {
		t.Fatalf("client body changed: %v", client)
	}

	set := map[string]any{"model": "local/m", "messages": []any{}, "stream_options": map[string]any{"include_usage": false}}
	res, _ = ex.Execute(context.Background(), cred, set, provider.ExecuteOptions{Stream: true})
	for range res.Stream {
	}
	// The meter needs the usage even when the client turned it off; the
	// translator keeps the usage chunk from that client.
	if includeUsage() != true {
		t.Fatalf("include_usage false reached upstream: %v", got["stream_options"])
	}
	if set["stream_options"].(map[string]any)["include_usage"] != false {
		t.Fatalf("client's stream_options changed: %v", set["stream_options"])
	}

	res, _ = ex.Execute(context.Background(), cred, map[string]any{"model": "local/m", "messages": []any{}}, provider.ExecuteOptions{})
	_ = res
	if _, ok := got["stream_options"]; ok {
		t.Fatalf("non-streamed request got stream_options: %v", got)
	}
}
