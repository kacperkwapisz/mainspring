package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type shortBody struct{ r io.Reader }

func (s shortBody) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

// A client that hangs up while sending its request is a cancellation, not a
// malformed request.
func TestClientHangingUpMidBodyIsACancellation(t *testing.T) {
	b := bootAPI(t)
	created, err := b.Hub.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", shortBody{strings.NewReader(`{"model":"claude-`)})
	req.Header.Set("x-api-key", created.Secret)
	req.Header.Set("content-type", "application/json")
	res := httptest.NewRecorder()
	b.Handler.ServeHTTP(res, req)
	if res.Code != 499 {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
}
