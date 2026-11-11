package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsArePublicAndUseTheRequestsAddress(t *testing.T) {
	b := bootAPIWith(t, map[string]string{"MAINSPRING_AUTH": "webauthn"})
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://hub.lan:8317"+path, nil)
		res := httptest.NewRecorder()
		b.Handler.ServeHTTP(res, req)
		return res
	}
	if res := get("/docs/connect"); res.Code != 200 || !strings.Contains(res.Body.String(), "http://hub.lan:8317/v1") || strings.Contains(res.Body.String(), "your-tailnet") {
		t.Fatalf("page: %d, address not filled in", res.Code)
	}
	if res := get("/docs/connect.md"); res.Code != 200 || !strings.HasPrefix(res.Body.String(), "# Connect your tools") || !strings.Contains(res.Header().Get("Content-Type"), "markdown") {
		t.Fatalf("markdown: %d %q", res.Code, res.Header().Get("Content-Type"))
	}
	if res := get("/llms.txt"); res.Code != 200 || !strings.Contains(res.Body.String(), "(http://hub.lan:8317/docs/api.md)") {
		t.Fatalf("llms.txt: %d", res.Code)
	}
	if res := get("/llms-full.txt"); res.Code != 200 || !strings.Contains(res.Body.String(), "# API") {
		t.Fatalf("llms-full.txt: %d", res.Code)
	}
	if res := get("/docs"); res.Code != http.StatusFound {
		t.Fatalf("/docs should redirect: %d", res.Code)
	}
	if res := get("/docs/nope"); res.Code != 404 {
		t.Fatalf("unknown page: %d", res.Code)
	}
	if res := get("/assets/docs.css"); res.Code != 200 {
		t.Fatalf("docs.css: %d", res.Code)
	}
}
