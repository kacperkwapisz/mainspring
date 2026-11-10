package app

import (
	"net/http"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/session"
)

// authPanel signs the request in to the panel. It is the one place tests
// depend on how panel sessions are issued.
func authPanel(t *testing.T, b *Booted, req *http.Request) {
	t.Helper()
	cookie := session.Issue(b.Env.JWTKey, false)
	if i := len("ms_session="); len(cookie) > i {
		end := len(cookie)
		for j := 0; j < len(cookie); j++ {
			if cookie[j] == ';' {
				end = j
				break
			}
		}
		req.Header.Set("Cookie", cookie[:end])
	}
}
