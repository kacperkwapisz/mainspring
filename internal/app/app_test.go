package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

const secret = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestHealthNoKey(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/health", nil))
	if res.Code != 200 {
		t.Fatalf("status %d", res.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body["ok"] != true {
		t.Fatalf("%s", res.Body.String())
	}
}

func TestModelsEmptyArrayNotNull(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	created, err := booted.Hub.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("authorization", "Bearer "+created.Secret)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d", res.Code)
	}
	body := res.Body.String()
	if !strings.Contains(body, `"data":[]`) {
		t.Fatalf("%s", body)
	}
	if strings.HasSuffix(body, "\n") {
		t.Fatalf("trailing newline %q", body)
	}
}

func TestModelsDiscoveryFields(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	if _, err := booted.Hub.Vault.Save(store.SaveCredential{
		Provider: domain.ProviderGrok, Label: "grok",
		Tokens: domain.CredentialTokens{AccessToken: "t", Extra: map[string]any{}},
	}); err != nil {
		t.Fatal(err)
	}
	created, err := booted.Hub.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("authorization", "Bearer "+created.Secret)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d", res.Code)
	}
	var body struct {
		Data []domain.ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var grok *domain.ModelInfo
	for i := range body.Data {
		if body.Data[i].ID == "grok-4.6" {
			grok = &body.Data[i]
			break
		}
	}
	if grok == nil {
		t.Fatalf("missing grok-4.6 in %s", res.Body.String())
	}
	if grok.ContextLength != 500000 || grok.MaxOutputTokens != 500000 {
		t.Fatalf("limits %+v", grok)
	}
	if grok.Cost == nil || grok.Cost.Input == nil || *grok.Cost.Input != 2 {
		t.Fatalf("cost %+v", grok.Cost)
	}
	if !grok.Reasoning || len(grok.Efforts) == 0 {
		t.Fatalf("reasoning %+v", grok)
	}
	if len(grok.InputModalities) == 0 || grok.InputModalities[0] != "text" {
		t.Fatalf("input %+v", grok.InputModalities)
	}
	var listed map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range listed["data"].([]any) {
		m := row.(map[string]any)
		if m["id"] != "grok-4.20-0309-non-reasoning" {
			continue
		}
		found = true
		if _, ok := m["reasoning"]; ok {
			t.Fatalf("non-reasoning still serializes reasoning: %+v", m)
		}
	}
	if !found {
		t.Fatal("missing non-reasoning")
	}
}

func TestModelsUnauthorized(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if res.Code != 401 {
		t.Fatalf("status %d", res.Code)
	}
}

func TestChatUnauthorizedLogsAndKeepsShape(t *testing.T) {
	var buf bytes.Buffer
	defer httpx.SetLogOutput(&buf)()
	httpx.SetLogLevel("info")
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"grok-4.6"}`))
	req.Header.Set("content-type", "application/json")
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatalf("status %d %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"type":"invalid_request_error"`) || !strings.Contains(res.Body.String(), "invalid api key") {
		t.Fatalf("%s", res.Body.String())
	}
	if !strings.Contains(buf.String(), "hub error") || !strings.Contains(buf.String(), "/v1/chat/completions") {
		t.Fatalf("log %q", buf.String())
	}
}

func TestChatNoCredentialHumanJSON(t *testing.T) {
	var buf bytes.Buffer
	defer httpx.SetLogOutput(&buf)()
	httpx.SetLogLevel("info")
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	created, err := booted.Hub.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"grok-4.6","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+created.Secret)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 503 {
		t.Fatalf("status %d %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "<html") {
		t.Fatalf("dump %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "no grok credential") || !strings.Contains(res.Body.String(), "panel") {
		t.Fatalf("%s", res.Body.String())
	}
	if !strings.Contains(buf.String(), "grok/grok-4.6") {
		t.Fatalf("log %q", buf.String())
	}
}

func TestPasswordRoutesGone(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	for _, path := range []string{"/password", "/login"} {
		res := httptest.NewRecorder()
		form := url.Values{"password": {"x"}, "old_password": {"nope"}, "new_password": {"long-enough"}}
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("content-type", "application/x-www-form-urlencoded")
		booted.Handler.ServeHTTP(res, req)
		if res.Code != 404 || res.Header().Get("set-cookie") != "" {
			t.Fatalf("%s %d %q", path, res.Code, res.Header().Get("set-cookie"))
		}
	}
}

func unlocked(t *testing.T) (*Booted, string) {
	t.Helper()
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := booted.Auth.IssueSession(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	cookie = strings.Split(cookie, ";")[0]
	if !strings.Contains(cookie, "ms_session=") {
		t.Fatalf("cookie %s", cookie)
	}
	return booted, cookie
}

type roundTrip func(*http.Request) *http.Response

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r), nil }

func jsonResp(status int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	rec.Header().Set("content-type", "application/json")
	rec.WriteHeader(status)
	_, _ = rec.Write(raw)
	res := rec.Result()
	res.Body = io.NopCloser(strings.NewReader(string(raw)))
	return res
}
