package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

type imageExec struct {
	fakeExec
	used   *[]string
	bodies *[]map[string]any
	reply  func(label string) *http.Response
}

func (e imageExec) Images(_ context.Context, c domain.Credential, edit bool, body map[string]any, _ map[string]string) (*http.Response, error) {
	op := "generate"
	if edit {
		op = "edit"
	}
	*e.used = append(*e.used, c.Label+"/"+op)
	*e.bodies = append(*e.bodies, body)
	return e.reply(c.Label), nil
}

func jsonReply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func imageHub(t *testing.T, reply func(string) *http.Response, labels ...string) (*Hub, domain.LocalKey, *[]string, *[]map[string]any, []string, func()) {
	return imageHubWith(t, reply, nil, labels...)
}

func imageHubWith(t *testing.T, reply func(string) *http.Response, refresh func(domain.Credential) (domain.Credential, error), labels ...string) (*Hub, domain.LocalKey, *[]string, *[]map[string]any, []string, func()) {
	t.Helper()
	var used []string
	var bodies []map[string]any
	ex := imageExec{fakeExec: fakeExec{id: domain.ProviderCodex, format: domain.FormatCodex, refresh: refresh}, used: &used, bodies: &bodies, reply: reply}
	hub, d := testHub(t, map[domain.ProviderID]provider.Executor{domain.ProviderCodex: ex})
	var ids []string
	for _, label := range labels {
		c, err := hub.Vault.Save(store.SaveCredential{Provider: domain.ProviderCodex, Label: label, Tokens: domain.CredentialTokens{AccessToken: "t"}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	created, _ := hub.Keys.Create("laptop", nil, nil, nil)
	key, _ := hub.Keys.Verify(created.Secret)
	return hub, *key, &used, &bodies, ids, func() { d.Close() }
}

const imageOK = `{"created":1,"data":[{"b64_json":"aGk="}],"usage":{"input_tokens":17,"output_tokens":1372}}`

func TestImagesPassTheAnswerOnAsItCame(t *testing.T) {
	hub, key, used, bodies, _, done := imageHub(t, func(string) *http.Response { return jsonReply(200, imageOK) }, "a")
	defer done()
	body := map[string]any{"model": "gpt-image-2", "prompt": "a fox", "size": "1024x1024", "quality": "low"}
	res, err := Images(context.Background(), hub, key, nil, false, body)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || string(res.Raw) != imageOK {
		t.Fatalf("want the upstream body as is, got %+v", res)
	}
	if len(*used) != 1 || (*used)[0] != "a/generate" {
		t.Fatalf("used %v", *used)
	}
	if got := (*bodies)[0]; got["size"] != "1024x1024" || got["quality"] != "low" || len(got) != 4 {
		t.Fatalf("the request should go upstream as sent, got %v", got)
	}
	if res.Header.Get("X-Mainspring-Provider") != "codex" {
		t.Fatalf("headers %v", res.Header)
	}
	totals, _ := hub.Usage.Since(60_000)
	if totals.Requests != 1 || totals.PromptTokens != 17 || totals.CompletionTokens != 1372 {
		t.Fatalf("meter %+v", totals)
	}
}

func TestImagesMoveOnWithoutRestingTheAccount(t *testing.T) {
	limited := `{"error":{"type":"usage_limit_reached","message":"You've hit your image limit","resets_in_seconds":3600}}`
	hub, key, used, _, ids, done := imageHub(t, func(label string) *http.Response {
		if label == "a" {
			return jsonReply(429, limited)
		}
		return jsonReply(200, imageOK)
	}, "a", "b")
	defer done()
	// Pin the caller's session to a, so a is tried first.
	hub.Router.Bind(domain.ProviderCodex, "sid:sess-1", ids[0])
	headers := map[string]string{"X-Session-ID": "sess-1"}
	res, err := Images(context.Background(), hub, key, headers, false, map[string]any{"model": "gpt-image-2", "prompt": "a fox"})
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	if strings.Join(*used, ",") != "a/generate,b/generate" {
		t.Fatalf("should try a, then b: %v", *used)
	}
	// Image quota is its own: a still takes chat requests.
	if st := hub.Router.Get(ids[0]); st.CoolUntil != 0 {
		t.Fatalf("a should not rest after an image 429: %+v", st)
	}
}

func TestImagesReturnTheProvidersErrorForABadRequest(t *testing.T) {
	hub, key, used, _, _, done := imageHub(t, func(string) *http.Response {
		return jsonReply(400, `{"error":{"type":"invalid_request_error","message":"Invalid size '7x7'."}}`)
	}, "a", "b")
	defer done()
	res, err := Images(context.Background(), hub, key, nil, true, map[string]any{"model": "gpt-image-2", "prompt": "x", "size": "7x7"})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Status != 400 || len(*used) != 1 || (*used)[0] != "a/edit" {
		t.Fatalf("a bad request shouldn't move on: %+v %v", res, *used)
	}
	if msg, _ := asMap(asMap(res.Body)["error"])["message"].(string); !strings.Contains(msg, "Invalid size") {
		t.Fatalf("want the provider's message, got %v", res.Body)
	}
}

func TestImagesNeedAModel(t *testing.T) {
	hub, key, used, _, _, done := imageHub(t, func(string) *http.Response { return jsonReply(200, imageOK) }, "a")
	defer done()
	res, _ := Images(context.Background(), hub, key, nil, false, map[string]any{"prompt": "x"})
	if res.OK || res.Status != 400 || len(*used) != 0 {
		t.Fatalf("%+v %v", res, *used)
	}
}

func TestImagesRelayAStreamAndMeterItsUsage(t *testing.T) {
	stream := "event: image_generation.partial_image\ndata: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"cA==\"}\n\n" +
		"event: image_generation.completed\ndata: {\"type\":\"image_generation.completed\",\"b64_json\":\"aGk=\",\"usage\":{\"input_tokens\":5,\"output_tokens\":9}}\n\n"
	hub, key, _, _, _, done := imageHub(t, func(string) *http.Response {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}
	}, "a")
	defer done()
	res, err := Images(context.Background(), hub, key, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x", "stream": true})
	if err != nil || !res.OK || res.Stream == nil {
		t.Fatalf("%+v %v", res, err)
	}
	var got strings.Builder
	for chunk := range res.Stream {
		got.WriteString(chunk)
	}
	if got.String() != stream {
		t.Fatalf("stream changed:\n%q\nwant\n%q", got.String(), stream)
	}
	totals, _ := hub.Usage.Since(60_000)
	if totals.Requests != 1 || totals.PromptTokens != 5 || totals.CompletionTokens != 9 {
		t.Fatalf("meter %+v", totals)
	}
}

func TestImagesDontMoveAPolicyRefusalToAnotherAccount(t *testing.T) {
	hub, key, used, _, _, done := imageHub(t, func(string) *http.Response {
		return jsonReply(403, `{"error":{"code":"misalignment_policy_violation","message":"This request was flagged."}}`)
	}, "a", "b")
	defer done()
	res, _ := Images(context.Background(), hub, key, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x"})
	if res.OK || res.Status != 403 || len(*used) != 1 {
		t.Fatalf("a 403 should come back without trying b: %+v %v", res, *used)
	}
}

func TestImagesUseAnAccountRestingForChat(t *testing.T) {
	hub, key, used, _, ids, done := imageHub(t, func(string) *http.Response { return jsonReply(200, imageOK) }, "a")
	defer done()
	// a's weekly chat window is spent; its image quota is its own.
	hub.Router.RateLimited(ids[0], route.Limit{Until: time.Now().Add(time.Hour).UnixMilli(), WindowID: "codex-secondary", Label: "7 days", Account: true})
	res, _ := Images(context.Background(), hub, key, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x"})
	if !res.OK || len(*used) != 1 {
		t.Fatalf("%+v %v", res, *used)
	}
	// A rejected token still keeps the account out.
	hub.Router.AuthRejected(ids[0])
	*used = nil
	res, _ = Images(context.Background(), hub, key, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x"})
	if res.OK || res.Status != 429 || len(*used) != 0 {
		t.Fatalf("%+v %v", res, *used)
	}
}

func TestImagesRefreshOnceAfterA401(t *testing.T) {
	calls := 0
	hub, key, used, _, ids, done := imageHubWith(t, func(string) *http.Response {
		calls++
		if calls == 1 {
			return jsonReply(401, `{"error":{"message":"token expired"}}`)
		}
		return jsonReply(200, imageOK)
	}, func(c domain.Credential) (domain.Credential, error) {
		c.Tokens.AccessToken = "fresh"
		return c, nil
	}, "a")
	defer done()
	res, _ := Images(context.Background(), hub, key, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x"})
	if !res.OK || len(*used) != 2 {
		t.Fatalf("should refresh and ask again: %+v %v", res, *used)
	}
	if st := hub.Router.Get(ids[0]); st.CoolUntil != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestImagesRestAnAccountWhoseRefreshFails(t *testing.T) {
	hub, key, _, _, ids, done := imageHubWith(t, func(string) *http.Response {
		return jsonReply(401, `{"error":{"message":"token expired"}}`)
	}, func(c domain.Credential) (domain.Credential, error) {
		return c, errors.New("network down")
	}, "a")
	defer done()
	res, _ := Images(context.Background(), hub, key, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x"})
	if res.OK || hub.Router.Get(ids[0]).CoolUntil == 0 {
		t.Fatalf("a should rest: %+v %+v", res, hub.Router.Get(ids[0]))
	}
}

func TestImagesRefuseAnAnswerThatIsntJSON(t *testing.T) {
	hub, key, _, _, _, done := imageHub(t, func(string) *http.Response { return jsonReply(200, "<html>") }, "a")
	defer done()
	res, _ := Images(context.Background(), hub, key, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x"})
	if res.OK || res.Status != 502 {
		t.Fatalf("%+v", res)
	}
}

func TestImagesKeepKeyScopesAndCaps(t *testing.T) {
	hub, _, used, _, _, done := imageHub(t, func(string) *http.Response { return jsonReply(200, imageOK) }, "a")
	defer done()
	created, _ := hub.Keys.Create("claude only", []domain.ProviderID{domain.ProviderClaude}, nil, nil)
	scoped, _ := hub.Keys.Verify(created.Secret)
	res, _ := Images(context.Background(), hub, *scoped, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x"})
	if res.OK || res.Status != 403 || len(*used) != 0 {
		t.Fatalf("a key without Codex can't make images: %+v", res)
	}
	zero := int64(0)
	created, _ = hub.Keys.Create("capped", nil, nil, &zero)
	capped, _ := hub.Keys.Verify(created.Secret)
	res, _ = Images(context.Background(), hub, *capped, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x"})
	if res.OK || res.Status != 429 || len(*used) != 0 {
		t.Fatalf("a key over its cap can't make images: %+v", res)
	}
	_ = SetModelEnabled(hub, "gpt-image-2", false)
	created, _ = hub.Keys.Create("any", nil, nil, nil)
	anyKey, _ := hub.Keys.Verify(created.Secret)
	res, _ = Images(context.Background(), hub, *anyKey, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x"})
	if res.OK || res.Status != 404 {
		t.Fatalf("a turned-off model is refused: %+v", res)
	}
}

func TestImagesMeterAStreamThatStopsShortAsAnError(t *testing.T) {
	stream := "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"cA==\"}\n\n"
	hub, key, _, _, _, done := imageHub(t, func(string) *http.Response {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}
	}, "a")
	defer done()
	res, _ := Images(context.Background(), hub, key, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x", "stream": true})
	for range res.Stream {
	}
	totals, _ := hub.Usage.Since(60_000)
	if totals.Requests != 1 || totals.Errors != 1 {
		t.Fatalf("meter %+v", totals)
	}
}

func TestImagesStopRelayingWhenTheClientLeaves(t *testing.T) {
	pr, pw := io.Pipe()
	hub, key, _, _, _, done := imageHub(t, func(string) *http.Response {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: pr}
	}, "a")
	defer done()
	ctx, cancel := context.WithCancel(context.Background())
	res, _ := Images(ctx, hub, key, nil, false, map[string]any{"model": "gpt-image-2", "prompt": "x", "stream": true})
	go func() { _, _ = pw.Write([]byte("data: {\"type\":\"image_generation.partial_image\"}\n\n")) }()
	cancel() // nobody reads the stream any more
	time.Sleep(50 * time.Millisecond)
	_, _ = pw.Write([]byte("data: {}\n\n"))
	pw.Close()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-res.Stream:
			if !ok {
				totals, _ := hub.Usage.Since(60_000)
				if totals.Errors != 1 {
					t.Fatalf("meter %+v", totals)
				}
				return
			}
		case <-deadline:
			t.Fatal("the relay should stop once the client is gone")
		}
	}
}
