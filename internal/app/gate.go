package app

import "net/http"

// Gate decides whether a request may use the operator panel and its JSON API.
// The API under /v1 is gated separately by LocalKeys.
type Gate interface {
	// Authed reports whether r carries a valid panel session.
	Authed(r *http.Request) bool
}

// requirePanel writes a 401 JSON error and returns false when r is not authed.
func requirePanel(w http.ResponseWriter, r *http.Request, g Gate) bool {
	if g != nil && g.Authed(r) {
		return true
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":{"code":"panel_signed_out","message":"Sign in to the panel first"}}`))
	return false
}
