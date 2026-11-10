package app

import (
	"net/http"
	"strings"
	"testing"
)

// authPanel signs the request in to the panel. It is the one place tests
// depend on how panel sessions are issued.
func authPanel(t *testing.T, b *Booted, req *http.Request) {
	t.Helper()
	cookie, err := b.Auth.IssueSession(req)
	if err != nil {
		t.Fatal(err)
	}
	if i := strings.IndexByte(cookie, ';'); i > 0 {
		cookie = cookie[:i]
	}
	req.Header.Set("Cookie", cookie)
}
