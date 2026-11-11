package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mcpCall(t *testing.T, b *Booted, auth, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res := httptest.NewRecorder()
	b.Handler.ServeHTTP(res, req)
	var out map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out
}

func TestMCPServesWebToolsToKeys(t *testing.T) {
	b := bootAPI(t)
	created, err := b.Hub.Keys.Create("laptop", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := mcpCall(t, b, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`); code != 401 {
		t.Fatalf("no key: %d", code)
	}
	code, init := mcpCall(t, b, created.Secret, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	result, _ := init["result"].(map[string]any)
	if code != 200 || result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize: %d %v", code, init)
	}
	if code, _ := mcpCall(t, b, created.Secret, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != http.StatusAccepted {
		t.Fatalf("notification: %d", code)
	}
	_, list := mcpCall(t, b, created.Secret, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw, _ := json.Marshal(list)
	if !strings.Contains(string(raw), `"web_search"`) || !strings.Contains(string(raw), `"web_fetch"`) {
		t.Fatalf("tools: %s", raw)
	}
	// No Claude or Codex account yet: a clear tool error, not a protocol error.
	_, call := mcpCall(t, b, created.Secret, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"web_search","arguments":{"query":"x"}}}`)
	res, _ := call["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("want a tool error: %v", call)
	}
	// Turned off in settings.
	_ = b.Hub.Settings.Set(SettingWebTools, "0")
	if code, _ := mcpCall(t, b, created.Secret, `{"jsonrpc":"2.0","id":4,"method":"tools/list"}`); code != 404 {
		t.Fatalf("turned off: %d", code)
	}
}
