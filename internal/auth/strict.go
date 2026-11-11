package auth

import (
	"context"
	"net/http"

	"github.com/kacperkwapisz/mainspring/internal/env"
)

// Mainspring's tailnet address isn't behind Umbrel's login (or any sign-in
// proxy), so requests that arrive there always need Mainspring's own sign-in,
// and forwarded headers from tailnet devices aren't trusted.

type strictKey struct{}

// Strict marks every request through next as needing Mainspring's own sign-in
// and drops X-Forwarded-* headers, which only a trusted proxy may set.
func Strict(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.Header {
			if len(name) > 12 && http.CanonicalHeaderKey(name)[:12] == "X-Forwarded-" {
				r.Header.Del(name)
			}
		}
		r.Header.Del("Forwarded")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), strictKey{}, true)))
	})
}

// IsStrict reports whether r came through Strict.
func IsStrict(r *http.Request) bool {
	v, _ := r.Context().Value(strictKey{}).(bool)
	return v
}

// KeyRequiredFor reports whether r needs a WebAuthn session.
func (s *Service) KeyRequiredFor(r *http.Request) bool {
	return IsStrict(r) || s.KeyRequired()
}

// ModeFor is the sign-in mode as r sees it: a strict request is never proxied.
func (s *Service) ModeFor(r *http.Request) string {
	if IsStrict(r) {
		return env.AuthWebAuthn
	}
	return s.Mode
}

// SetupNeededFor reports whether r must add the first key with a setup code.
func (s *Service) SetupNeededFor(r *http.Request) (bool, error) {
	if !s.KeyRequiredFor(r) {
		return false, nil
	}
	n, err := s.Creds.Count()
	return n == 0, err
}
