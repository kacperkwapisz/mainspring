package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

type wsFakeCall struct {
	body    map[string]any
	headers map[string]string
}

type wsFakeExec struct {
	mu    sync.Mutex
	calls []wsFakeCall
	fn    func(ctx context.Context, n int) provider.ExecuteResult
}

func (f *wsFakeExec) ID() domain.ProviderID         { return domain.ProviderCodex }
func (f *wsFakeExec) Format() domain.ExecutorFormat { return domain.FormatCodex }
func (f *wsFakeExec) Models() []domain.ModelInfo {
	return []domain.ModelInfo{{ID: "gpt-5.5", Object: "model", OwnedBy: "codex"}}
}
func (f *wsFakeExec) Refresh(_ context.Context, c domain.Credential) (domain.Credential, error) {
	return c, nil
}
func (f *wsFakeExec) Execute(ctx context.Context, _ domain.Credential, body any, opts provider.ExecuteOptions) (provider.ExecuteResult, error) {
	raw, _ := json.Marshal(body)
	var copied map[string]any
	_ = json.Unmarshal(raw, &copied)
	f.mu.Lock()
	f.calls = append(f.calls, wsFakeCall{body: copied, headers: opts.InboundHeaders})
	n := len(f.calls)
	f.mu.Unlock()
	return f.fn(ctx, n), nil
}

func (f *wsFakeExec) call(i int) wsFakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[i]
}

func (f *wsFakeExec) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// cannedStream streams a short Codex turn whose response id is id.
func cannedStream(ctx context.Context, id, text string) <-chan any {
	events := []any{
		map[string]any{"type": "response.created", "response": map[string]any{"id": id, "object": "response", "status": "in_progress"}},
		map[string]any{"type": "response.output_text.delta", "delta": text},
		map[string]any{"type": "response.output_item.done", "item": map[string]any{
			"type": "message", "role": "assistant", "id": "msg_" + id,
			"content": []any{map[string]any{"type": "output_text", "text": text}},
		}},
		map[string]any{"type": "response.completed", "response": map[string]any{
			"id": id, "object": "response", "status": "completed",
			"usage": map[string]any{"input_tokens": 3.0, "output_tokens": 2.0},
		}},
	}
	ch := make(chan any)
	go func() {
		defer close(ch)
		for _, ev := range events {
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

type wsEnv struct {
	booted *Booted
	srv    *httptest.Server
	secret string
	exec   *wsFakeExec
}

func newWSEnv(t *testing.T, fn func(ctx context.Context, n int) provider.ExecuteResult) *wsEnv {
	t.Helper()
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	exec := &wsFakeExec{fn: fn}
	booted.Hub.Executors[domain.ProviderCodex] = exec
	if _, err := booted.Hub.Vault.Save(store.SaveCredential{
		ID: "cx1", Provider: domain.ProviderCodex, Label: "Codex", Tokens: domain.CredentialTokens{AccessToken: "t"},
	}); err != nil {
		t.Fatal(err)
	}
	created, err := booted.Hub.Keys.Create("ws", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(booted.Handler)
	t.Cleanup(func() {
		srv.Close()
		booted.DB.Close()
	})
	return &wsEnv{booted: booted, srv: srv, secret: created.Secret, exec: exec}
}

func (e *wsEnv) url() string { return "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/v1/responses" }

func (e *wsEnv) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	h.Set("Authorization", "Bearer "+e.secret)
	h.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
	h.Set("session-id", "sess-ws-1")
	conn, res, err := websocket.Dial(ctx, e.url(), &websocket.DialOptions{
		HTTPHeader: h, CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		status := 0
		if res != nil {
			status = res.StatusCode
		}
		t.Fatalf("dial: %v (status %d)", err, status)
	}
	if ext := res.Header.Get("Sec-WebSocket-Extensions"); !strings.Contains(ext, "permessage-deflate") {
		t.Fatalf("compression not negotiated: %q", ext)
	}
	conn.SetReadLimit(32 << 20)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func wsSend(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	raw, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatal(err)
	}
}

func wsRecv(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("message type %v", typ)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("frame %s: %v", raw, err)
	}
	return out
}

// wsReadTurn reads frames until the turn ends and returns their types.
func wsReadTurn(t *testing.T, conn *websocket.Conn) ([]string, []map[string]any) {
	t.Helper()
	var types []string
	var frames []map[string]any
	for {
		f := wsRecv(t, conn)
		typ, _ := f["type"].(string)
		types = append(types, typ)
		frames = append(frames, f)
		switch typ {
		case "response.completed", "response.incomplete", "response.failed", "error":
			return types, frames
		}
	}
}

func userMsg(text string) map[string]any {
	return map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}
}

func inputTexts(t *testing.T, body map[string]any) []string {
	t.Helper()
	items, _ := body["input"].([]any)
	var out []string
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		for _, c := range item["content"].([]any) {
			out = append(out, item["role"].(string)+":"+c.(map[string]any)["text"].(string))
		}
	}
	return out
}

func okStream(ctx context.Context, n int) provider.ExecuteResult {
	id := "resp_up_" + string(rune('0'+n))
	return provider.ExecuteResult{OK: true, Status: 200, Stream: cannedStream(ctx, id, "answer"+string(rune('0'+n)))}
}

func TestWSRejectsUnauthenticatedUpgrade(t *testing.T) {
	env := newWSEnv(t, okStream)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-ms-nope")
	_, res, err := websocket.Dial(ctx, env.url(), &websocket.DialOptions{HTTPHeader: h})
	if err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err %v res %+v", err, res)
	}
	_, res, err = websocket.Dial(ctx, env.url(), nil)
	if err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no key: err %v res %+v", err, res)
	}
}

func TestWSPlainGETIs405(t *testing.T) {
	env := newWSEnv(t, okStream)
	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer "+env.secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed || !strings.HasPrefix(res.Header.Get("content-type"), "application/json") {
		t.Fatalf("status %d ct %s", res.StatusCode, res.Header.Get("content-type"))
	}
}

func TestWSDisabledIs426(t *testing.T) {
	env := newWSEnv(t, okStream)
	if err := env.booted.Hub.Settings.Set(SettingWSEnabled, "0"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	h.Set("Authorization", "Bearer "+env.secret)
	_, res, err := websocket.Dial(ctx, env.url(), &websocket.DialOptions{HTTPHeader: h})
	if err == nil || res == nil || res.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("err %v res %+v", err, res)
	}
}

func TestWSFullTurnStreamsFrames(t *testing.T) {
	env := newWSEnv(t, okStream)
	conn := env.dial(t)
	wsSend(t, conn, map[string]any{
		"type": "response.create", "model": "gpt-5.5", "instructions": "be brief",
		"input":           []any{userMsg("hi")},
		"client_metadata": map[string]any{"x-codex-turn-state": "ts-1", "other": "x"},
	})
	types, frames := wsReadTurn(t, conn)
	want := []string{"response.created", "response.output_text.delta", "response.output_item.done", "response.completed"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("types %v", types)
	}
	if id := frames[3]["response"].(map[string]any)["id"]; id != "resp_up_1" {
		t.Fatalf("completed id %v", id)
	}
	call := env.exec.call(0)
	if call.body["type"] != nil || call.body["client_metadata"] != nil || call.body["stream"] != true {
		t.Fatalf("body %v", call.body)
	}
	if call.headers["x-codex-turn-state"] != "ts-1" || call.headers["Session-Id"] != "sess-ws-1" {
		t.Fatalf("headers %v", call.headers)
	}
	// The socket stays open for the next turn.
	wsSend(t, conn, map[string]any{"type": "response.create", "model": "gpt-5.5", "input": []any{userMsg("again")}})
	if types, _ := wsReadTurn(t, conn); types[len(types)-1] != "response.completed" {
		t.Fatalf("second turn %v", types)
	}
}

func TestWSPrewarmThenIncrementalRebuildsInput(t *testing.T) {
	env := newWSEnv(t, okStream)
	conn := env.dial(t)
	wsSend(t, conn, map[string]any{
		"type": "response.create", "model": "gpt-5.5", "generate": false,
		"input": []any{userMsg("first")},
	})
	created := wsRecv(t, conn)
	completed := wsRecv(t, conn)
	if created["type"] != "response.created" || completed["type"] != "response.completed" {
		t.Fatalf("%v %v", created, completed)
	}
	prewarmID, _ := completed["response"].(map[string]any)["id"].(string)
	if !strings.HasPrefix(prewarmID, "resp_ws_") || created["response"].(map[string]any)["id"] != prewarmID {
		t.Fatalf("prewarm id %q", prewarmID)
	}
	if env.exec.count() != 0 {
		t.Fatal("prewarm reached upstream")
	}

	wsSend(t, conn, map[string]any{
		"type": "response.create", "model": "gpt-5.5", "previous_response_id": prewarmID,
		"input": []any{userMsg("second")},
	})
	if types, _ := wsReadTurn(t, conn); types[len(types)-1] != "response.completed" {
		t.Fatalf("turn 1 %v", types)
	}
	got := inputTexts(t, env.exec.call(0).body)
	if strings.Join(got, "|") != "user:first|user:second" {
		t.Fatalf("turn 1 input %v", got)
	}
	if env.exec.call(0).body["previous_response_id"] != nil || env.exec.call(0).body["generate"] != nil {
		t.Fatalf("body %v", env.exec.call(0).body)
	}

	wsSend(t, conn, map[string]any{
		"type": "response.create", "model": "gpt-5.5", "previous_response_id": "resp_up_1",
		"input": []any{userMsg("third")},
	})
	if types, _ := wsReadTurn(t, conn); types[len(types)-1] != "response.completed" {
		t.Fatalf("turn 2 %v", types)
	}
	got = inputTexts(t, env.exec.call(1).body)
	if strings.Join(got, "|") != "user:first|user:second|assistant:answer1|user:third" {
		t.Fatalf("turn 2 input %v", got)
	}
}

func TestWSUnknownPreviousResponse(t *testing.T) {
	env := newWSEnv(t, okStream)
	conn := env.dial(t)
	wsSend(t, conn, map[string]any{
		"type": "response.create", "model": "gpt-5.5", "previous_response_id": "resp_missing",
		"input": []any{userMsg("hi")},
	})
	f := wsRecv(t, conn)
	errObj, _ := f["error"].(map[string]any)
	if f["type"] != "error" || f["status"] != 400.0 || errObj["code"] != "previous_response_not_found" {
		t.Fatalf("%v", f)
	}
	if env.exec.count() != 0 {
		t.Fatal("reached upstream")
	}
}

func TestWSInvalidMessageKeepsSocket(t *testing.T) {
	env := newWSEnv(t, okStream)
	conn := env.dial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	f := wsRecv(t, conn)
	if f["type"] != "error" || f["status"] != 400.0 || f["error"].(map[string]any)["code"] != "invalid_request" {
		t.Fatalf("%v", f)
	}
	wsSend(t, conn, map[string]any{"type": "response.create", "model": "gpt-5.5", "input": []any{userMsg("hi")}})
	if types, _ := wsReadTurn(t, conn); types[len(types)-1] != "response.completed" {
		t.Fatalf("%v", types)
	}
}

func TestWSUpstreamFailureSendsErrorFrame(t *testing.T) {
	env := newWSEnv(t, func(context.Context, int) provider.ExecuteResult {
		return provider.ExecuteResult{OK: false, Status: 429, Message: "rate limited"}
	})
	conn := env.dial(t)
	wsSend(t, conn, map[string]any{"type": "response.create", "model": "gpt-5.5", "input": []any{userMsg("hi")}})
	f := wsRecv(t, conn)
	errObj, _ := f["error"].(map[string]any)
	if f["type"] != "error" || f["status"] != 429.0 || errObj["message"] == nil || errObj["message"] == "" {
		t.Fatalf("%v", f)
	}
	// The socket survives the failure.
	wsSend(t, conn, map[string]any{"type": "response.create", "model": "gpt-5.5", "input": []any{userMsg("hi")}})
	if f := wsRecv(t, conn); f["type"] != "error" || f["status"] != 429.0 {
		t.Fatalf("%v", f)
	}
}

func TestWSSecondCreateWhileInFlight(t *testing.T) {
	release := make(chan struct{})
	env := newWSEnv(t, func(ctx context.Context, n int) provider.ExecuteResult {
		ch := make(chan any)
		go func() {
			defer close(ch)
			select {
			case ch <- map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_slow", "status": "in_progress"}}:
			case <-ctx.Done():
				return
			}
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
			for ev := range cannedStream(ctx, "resp_slow", "done") {
				if m := ev.(map[string]any); m["type"] == "response.created" {
					continue
				}
				select {
				case ch <- ev:
				case <-ctx.Done():
					return
				}
			}
		}()
		return provider.ExecuteResult{OK: true, Status: 200, Stream: ch}
	})
	conn := env.dial(t)
	wsSend(t, conn, map[string]any{"type": "response.create", "model": "gpt-5.5", "input": []any{userMsg("one")}})
	if f := wsRecv(t, conn); f["type"] != "response.created" {
		t.Fatalf("%v", f)
	}
	wsSend(t, conn, map[string]any{"type": "response.create", "model": "gpt-5.5", "input": []any{userMsg("two")}})
	f := wsRecv(t, conn)
	if f["type"] != "error" || f["status"] != 409.0 || f["error"].(map[string]any)["code"] != "turn_in_progress" {
		t.Fatalf("%v", f)
	}
	close(release)
	if types, _ := wsReadTurn(t, conn); types[len(types)-1] != "response.completed" {
		t.Fatalf("%v", types)
	}
	if env.exec.count() != 1 {
		t.Fatalf("calls %d", env.exec.count())
	}
}

func TestWSInterrupt(t *testing.T) {
	env := newWSEnv(t, func(ctx context.Context, n int) provider.ExecuteResult {
		if n > 1 {
			return okStream(ctx, n)
		}
		ch := make(chan any)
		go func() {
			defer close(ch)
			select {
			case ch <- map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_hang", "status": "in_progress"}}:
			case <-ctx.Done():
				return
			}
			<-ctx.Done()
		}()
		return provider.ExecuteResult{OK: true, Status: 200, Stream: ch}
	})
	conn := env.dial(t)
	wsSend(t, conn, map[string]any{"type": "response.interrupt", "response_id": "resp_none"})
	if f := wsRecv(t, conn); f["type"] != "response.interrupt.failed" {
		t.Fatalf("%v", f)
	}
	wsSend(t, conn, map[string]any{"type": "response.create", "model": "gpt-5.5", "input": []any{userMsg("one")}})
	if f := wsRecv(t, conn); f["type"] != "response.created" {
		t.Fatalf("%v", f)
	}
	wsSend(t, conn, map[string]any{"type": "response.interrupt", "response_id": "resp_hang"})
	if f := wsRecv(t, conn); f["type"] != "response.interrupt.accepted" || f["response_id"] != "resp_hang" {
		t.Fatalf("%v", f)
	}
	wsSend(t, conn, map[string]any{"type": "response.create", "model": "gpt-5.5", "input": []any{userMsg("two")}})
	if types, _ := wsReadTurn(t, conn); types[len(types)-1] != "response.completed" {
		t.Fatalf("%v", types)
	}
}

func TestWSLifetimeLimitCloses(t *testing.T) {
	prev := wsLifetime
	wsLifetime = 300 * time.Millisecond
	defer func() { wsLifetime = prev }()
	env := newWSEnv(t, okStream)
	conn := env.dial(t)
	f := wsRecv(t, conn)
	if f["type"] != "error" || f["error"].(map[string]any)["code"] != "websocket_connection_limit_reached" {
		t.Fatalf("%v", f)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("close %v", err)
	}
}

func TestWSLifetimeWaitsForInFlightTurn(t *testing.T) {
	prev := wsLifetime
	wsLifetime = 200 * time.Millisecond
	defer func() { wsLifetime = prev }()
	release := make(chan struct{})
	env := newWSEnv(t, func(ctx context.Context, n int) provider.ExecuteResult {
		ch := make(chan any)
		go func() {
			defer close(ch)
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
			for ev := range cannedStream(ctx, "resp_late", "late") {
				select {
				case ch <- ev:
				case <-ctx.Done():
					return
				}
			}
		}()
		return provider.ExecuteResult{OK: true, Status: 200, Stream: ch}
	})
	conn := env.dial(t)
	wsSend(t, conn, map[string]any{"type": "response.create", "model": "gpt-5.5", "input": []any{userMsg("hi")}})
	time.Sleep(400 * time.Millisecond)
	close(release)
	if types, _ := wsReadTurn(t, conn); types[len(types)-1] != "response.completed" {
		t.Fatalf("%v", types)
	}
	f := wsRecv(t, conn)
	if f["type"] != "error" || f["error"].(map[string]any)["code"] != "websocket_connection_limit_reached" {
		t.Fatalf("%v", f)
	}
}

func TestWSClientMetadataHeadersBecomeHeaders(t *testing.T) {
	env := newWSEnv(t, okStream)
	conn := env.dial(t)
	wsSend(t, conn, map[string]any{
		"type": "response.create", "model": "gpt-5.5", "input": []any{userMsg("hi")},
		"client_metadata": map[string]any{"ws_request_header_x_openai_internal_codex_responses_lite": "true"},
	})
	wsReadTurn(t, conn)
	if got := env.exec.call(0).headers["x-openai-internal-codex-responses-lite"]; got != "true" {
		t.Fatalf("lite header %q in %v", got, env.exec.call(0).headers)
	}
}
