package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
	"github.com/kacperkwapisz/mainspring/internal/translate"
)

// SettingWSEnabled turns the Responses WebSocket endpoint on or off. Any value
// other than "0" (including unset) leaves it on. When off, upgrades get 426 so
// Codex falls back to HTTP for the rest of its session.
const SettingWSEnabled = "ws.enabled"

const (
	wsReadLimit    = 32 << 20
	wsWriteTimeout = time.Minute
	wsTurnStateKey = "x-codex-turn-state"
)

// Tunable in tests.
var (
	wsPingInterval = 30 * time.Second
	wsLifetime     = 55 * time.Minute
	// wsLifetimeGrace bounds how long a turn still in flight at wsLifetime may keep the socket open.
	wsLifetimeGrace = 5 * time.Minute
)

// wsDropHeaders are handshake-only headers that mean nothing to an upstream request.
var wsDropHeaders = []string{
	"Upgrade", "Connection", "Sec-Websocket-Key", "Sec-Websocket-Version",
	"Sec-Websocket-Extensions", "Sec-Websocket-Protocol",
}

func registerResponsesWS(mux *httpx.Mux, hub *proxy.Hub) {
	mux.Handle(http.MethodGet, "/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		inbound := string(domain.InboundOpenAIResponses)
		if !isWebSocketUpgrade(r) {
			f := httpx.Fail{
				Kind: httpx.KindRequest, Status: http.StatusMethodNotAllowed, Type: "invalid_request_error",
				Route: r.URL.Path, Message: "GET /v1/responses only accepts WebSocket upgrades",
				Hint: "POST /v1/responses, or open a WebSocket",
			}
			f.Log()
			w.Header().Set("Allow", "GET, POST")
			httpx.JSON(w, http.StatusMethodNotAllowed, f.ClientBody(inbound))
			return
		}
		key := requireLocalKey(r, hub)
		if key == nil {
			writeUnauthorized(w, r, inbound)
			return
		}
		if !wsEnabled(hub) {
			httpx.JSON(w, http.StatusUpgradeRequired, map[string]any{"error": map[string]any{
				"type": "invalid_request_error", "code": "websockets_disabled",
				"message": "WebSockets are turned off on this hub. Use HTTP.",
			}})
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			CompressionMode: websocket.CompressionContextTakeover,
		})
		if err != nil {
			// Accept has already written the HTTP error response.
			return
		}
		conn.SetReadLimit(wsReadLimit)
		headers := httpx.InboundHeaders(r)
		for _, h := range wsDropHeaders {
			delete(headers, h)
		}
		s := &wsSession{conn: conn, hub: hub, key: *key, headers: headers, turnDone: make(chan struct{}, 1)}
		s.serve(r.Context())
	})
}

func wsEnabled(hub *proxy.Hub) bool {
	if hub.Settings == nil {
		return true
	}
	v, ok := hub.Settings.Get(SettingWSEnabled)
	return !ok || strings.TrimSpace(v) != "0"
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}

type wsSession struct {
	conn    *websocket.Conn
	hub     *proxy.Hub
	key     domain.LocalKey
	headers map[string]string
	ctx     context.Context
	wg      sync.WaitGroup

	mu         sync.Mutex
	inflight   *wsTurn
	closing    bool
	lastID     string
	lastInput  []any
	lastOutput []any

	turnDone  chan struct{}
	closeOnce sync.Once
}

type wsTurn struct {
	ctx        context.Context
	cancel     context.CancelFunc
	responseID string
}

func (s *wsSession) serve(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.ctx = ctx
	go s.keepalive(ctx)
	for {
		typ, data, err := s.conn.Read(ctx)
		if err != nil {
			break
		}
		if typ != websocket.MessageText {
			s.sendError(http.StatusBadRequest, wsErr("invalid_request_error", "invalid_request", "send JSON text messages"))
			continue
		}
		s.handle(data)
	}
	cancel()
	s.wg.Wait()
	_ = s.conn.CloseNow()
}

// keepalive pings every wsPingInterval and closes the socket once it reaches wsLifetime.
func (s *wsSession) keepalive(ctx context.Context) {
	ping := time.NewTicker(wsPingInterval)
	defer ping.Stop()
	life := time.NewTimer(wsLifetime)
	defer life.Stop()
	var grace <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, wsPingInterval)
			err := s.conn.Ping(pctx)
			cancel()
			// A missing pong only times out; a write that fails or stalls closes the conn.
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				_ = s.conn.CloseNow()
				return
			}
		case <-life.C:
			s.mu.Lock()
			s.closing = true
			busy := s.inflight != nil
			s.mu.Unlock()
			if !busy {
				s.limitClose()
				return
			}
			t := time.NewTimer(wsLifetimeGrace)
			defer t.Stop()
			grace = t.C
		case <-grace:
			s.limitClose()
			return
		case <-s.turnDone:
			s.mu.Lock()
			closeNow := s.closing && s.inflight == nil
			s.mu.Unlock()
			if closeNow {
				s.limitClose()
				return
			}
		}
	}
}

// limitClose sends the error Codex treats as "reconnect" and closes with 1000.
func (s *wsSession) limitClose() {
	s.closeOnce.Do(func() {
		s.sendError(http.StatusBadRequest, wsErr("invalid_request_error", "websocket_connection_limit_reached",
			"Responses websocket connection limit reached. Create a new websocket connection to continue."))
		_ = s.conn.Close(websocket.StatusNormalClosure, "connection limit reached")
	})
}

func (s *wsSession) handle(data []byte) {
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil || msg == nil {
		s.invalid("message is not a JSON object")
		return
	}
	switch msg["type"] {
	case "response.interrupt":
		s.interrupt(msg)
	case "response.create":
		s.create(msg)
	default:
		s.invalid("unknown message type")
	}
}

func (s *wsSession) invalid(message string) {
	httpx.Fail{
		Kind: httpx.KindRequest, Status: http.StatusBadRequest, Type: "invalid_request_error",
		Route: "GET /v1/responses (ws)", Message: message,
	}.Log()
	s.sendError(http.StatusBadRequest, wsErr("invalid_request_error", "invalid_request", message))
}

func (s *wsSession) interrupt(msg map[string]any) {
	id, _ := msg["response_id"].(string)
	s.mu.Lock()
	t := s.inflight
	ok := t != nil && (id == "" || t.responseID == "" || t.responseID == id)
	if ok {
		s.inflight = nil
	}
	s.mu.Unlock()
	if !ok {
		s.send(map[string]any{"type": "response.interrupt.failed", "response_id": id})
		return
	}
	t.cancel()
	s.signalTurnDone()
	s.send(map[string]any{"type": "response.interrupt.accepted", "response_id": id})
}

func (s *wsSession) create(body map[string]any) {
	delete(body, "type")
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		s.limitClose()
		return
	}
	if s.inflight != nil {
		s.mu.Unlock()
		s.sendError(http.StatusConflict, wsErr("invalid_request_error", "turn_in_progress",
			"A response is already in progress on this connection."))
		return
	}
	input := wsInputItems(body["input"])
	if prev, has := body["previous_response_id"]; has && prev != nil {
		id, _ := prev.(string)
		if id == "" || id != s.lastID {
			s.mu.Unlock()
			s.sendError(http.StatusBadRequest, wsErr("invalid_request_error", "previous_response_not_found",
				"Previous response with id '"+id+"' not found."))
			return
		}
		full := make([]any, 0, len(s.lastInput)+len(s.lastOutput)+len(input))
		full = append(full, s.lastInput...)
		full = append(full, s.lastOutput...)
		full = append(full, input...)
		input = full
	}
	delete(body, "previous_response_id")
	body["input"] = input

	if g, ok := body["generate"].(bool); ok && !g {
		id := wsResponseID()
		s.lastID = id
		s.lastInput = translate.AsArr(translate.CloneJSON(input))
		s.lastOutput = []any{}
		s.mu.Unlock()
		for _, typ := range []string{"response.created", "response.completed"} {
			s.send(map[string]any{"type": typ, "response": map[string]any{
				"id": id, "object": "response", "status": "completed", "output": []any{},
			}})
		}
		return
	}
	delete(body, "generate")

	headers := make(map[string]string, len(s.headers)+1)
	for k, v := range s.headers {
		headers[k] = v
	}
	if meta, ok := body["client_metadata"].(map[string]any); ok {
		if v, ok := meta[wsTurnStateKey].(string); ok && v != "" {
			headers[wsTurnStateKey] = v
		}
	}
	delete(body, "client_metadata")
	body["stream"] = true

	ctx, cancel := context.WithCancel(s.ctx)
	t := &wsTurn{ctx: ctx, cancel: cancel}
	s.inflight = t
	s.mu.Unlock()

	used := translate.AsArr(translate.CloneJSON(input))
	s.wg.Add(1)
	go s.runTurn(t, body, used, headers)
}

func (s *wsSession) runTurn(t *wsTurn, body map[string]any, used []any, headers map[string]string) {
	defer s.wg.Done()
	defer t.cancel()
	defer s.finish(t, nil)

	result, err := proxy.Proxy(t.ctx, s.hub, proxy.Request{
		Inbound: domain.InboundOpenAIResponses, Body: body, Key: s.key,
		Stream: true, InboundHeaders: headers,
	})
	if err != nil {
		status, kind, msg := httpx.ClassifyErr(err)
		_, typ, message, hint := httpx.Humanize(status, "", msg)
		f := httpx.Fail{Kind: kind, Status: status, Type: typ, Route: string(domain.InboundOpenAIResponses), Message: message, Hint: hint}
		f.Log()
		if s.finish(t, nil) {
			s.sendError(status, wsErrorObject(f.ClientBody(string(domain.InboundOpenAIResponses))))
		}
		return
	}
	if !result.OK {
		if s.finish(t, nil) {
			s.sendError(result.Status, wsErrorObject(result.Body))
		}
		return
	}
	if result.Stream == nil {
		resp, _ := result.Body.(map[string]any)
		rec := &wsRecord{input: used, output: translate.AsArr(resp["output"])}
		rec.id, _ = resp["id"].(string)
		if s.finish(t, rec) {
			s.send(map[string]any{"type": "response.completed", "response": resp})
		}
		return
	}

	var output []any
	terminal := false
	for chunk := range result.Stream {
		// Keep draining after an interrupt or a terminal event so the proxy goroutine can finish.
		if terminal || t.ctx.Err() != nil {
			continue
		}
		for _, payload := range sseDataPayloads(chunk) {
			if terminal || t.ctx.Err() != nil {
				break
			}
			var ev map[string]any
			_ = json.Unmarshal(payload, &ev)
			switch ev["type"] {
			case "response.created":
				id, _ := translate.AsMap(ev["response"])["id"].(string)
				s.mu.Lock()
				t.responseID = id
				s.mu.Unlock()
			case "response.output_item.done":
				if item, ok := ev["item"]; ok && item != nil {
					output = append(output, translate.CloneJSON(item))
				}
			case "response.completed", "response.incomplete":
				terminal = true
				rec := &wsRecord{input: used, output: output}
				rec.id, _ = translate.AsMap(ev["response"])["id"].(string)
				// Record before writing: Codex may send the next turn as soon as it sees this frame.
				if !s.finish(t, rec) {
					continue
				}
			case "response.failed", "error":
				terminal = true
				if !s.finish(t, nil) {
					continue
				}
			}
			if err := s.write(payload); err != nil {
				t.cancel()
				_ = s.conn.CloseNow()
			}
		}
	}
	if !terminal && s.finish(t, nil) {
		s.sendError(http.StatusBadGateway, wsErr("server_error", "stream_incomplete",
			"The upstream stream ended before the response completed."))
	}
}

type wsRecord struct {
	id     string
	input  []any
	output []any
}

// finish clears t as the in-flight turn and, when rec is set, stores it as the
// socket's last response. It reports whether t was still in flight (so the
// caller may write its closing frame).
func (s *wsSession) finish(t *wsTurn, rec *wsRecord) bool {
	s.mu.Lock()
	if s.inflight != t {
		s.mu.Unlock()
		return false
	}
	s.inflight = nil
	if rec != nil {
		s.lastID = rec.id
		s.lastInput = rec.input
		s.lastOutput = rec.output
		if s.lastOutput == nil {
			s.lastOutput = []any{}
		}
	}
	s.mu.Unlock()
	s.signalTurnDone()
	return true
}

func (s *wsSession) signalTurnDone() {
	select {
	case s.turnDone <- struct{}{}:
	default:
	}
}

func (s *wsSession) write(b []byte) error {
	ctx, cancel := context.WithTimeout(s.ctx, wsWriteTimeout)
	defer cancel()
	return s.conn.Write(ctx, websocket.MessageText, b)
}

func (s *wsSession) send(v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = s.write(raw)
}

func (s *wsSession) sendError(status int, errObj map[string]any) {
	s.send(map[string]any{"type": "error", "status": status, "error": errObj})
}

func wsErr(typ, code, message string) map[string]any {
	return map[string]any{"type": typ, "code": code, "message": message}
}

// wsErrorObject pulls the inner error object out of an HTTP error body.
func wsErrorObject(body any) map[string]any {
	if m, ok := body.(map[string]any); ok {
		if e, ok := m["error"].(map[string]any); ok {
			return e
		}
	}
	msg := httpx.ExtractErrorMessage(body)
	if msg == "" {
		msg = "request failed"
	}
	return map[string]any{"message": msg}
}

// sseDataPayloads returns the data payload of each SSE event in chunk, skipping
// event names, comments and [DONE].
func sseDataPayloads(chunk string) [][]byte {
	var out [][]byte
	for _, block := range strings.Split(strings.ReplaceAll(chunk, "\r\n", "\n"), "\n\n") {
		var data []string
		for _, line := range strings.Split(block, "\n") {
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		if len(data) == 0 {
			continue
		}
		payload := strings.Join(data, "\n")
		if trimmed := strings.TrimSpace(payload); trimmed == "" || trimmed == "[DONE]" {
			continue
		}
		out = append(out, bytes.Clone([]byte(payload)))
	}
	return out
}

func wsInputItems(v any) []any {
	switch x := v.(type) {
	case nil:
		return []any{}
	case []any:
		return x
	case string:
		if x == "" {
			return []any{}
		}
		return []any{map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": x}},
		}}
	default:
		return []any{x}
	}
}

func wsResponseID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return "resp_ws_" + hex.EncodeToString(buf)
}
