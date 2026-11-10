package app

import (
	"net/http"

	"github.com/kacperkwapisz/mainspring/internal/env"
	"github.com/kacperkwapisz/mainspring/internal/session"
)

// cookieGate accepts the signed panel session cookie. It is replaced by the
// WebAuthn gate; kept so the API is usable while that lands.
type cookieGate struct{ e *env.Env }

func (g cookieGate) Authed(r *http.Request) bool { return session.Read(r, g.e.JWTKey) }
