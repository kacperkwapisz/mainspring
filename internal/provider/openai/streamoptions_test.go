package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
)

// A streamed Chat request reaches the source with the client's stream_options
// as sent: none added when the client sent none, and include_usage false kept.
func TestStreamOptionsGoUpstreamAsSent(t *testing.T) {
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
	send := func(body map[string]any) {
		t.Helper()
		res, err := ex.Execute(context.Background(), cred, body, provider.ExecuteOptions{Stream: true})
		if err != nil || !res.OK {
			t.Fatalf("%+v %v", res, err)
		}
		for range res.Stream {
		}
	}

	send(map[string]any{"model": "local/m", "messages": []any{}})
	if so, ok := got["stream_options"]; ok {
		t.Fatalf("stream_options added: %v", so)
	}

	off := map[string]any{"include_usage": false}
	send(map[string]any{"model": "local/m", "messages": []any{}, "stream_options": off})
	if !reflect.DeepEqual(got["stream_options"], off) {
		t.Fatalf("stream_options changed: %v", got["stream_options"])
	}
}
