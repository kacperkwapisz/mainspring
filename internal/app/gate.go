package app

import (
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// Gate decides whether a request may use the operator panel and its JSON API.
// The API under /v1 is gated separately by LocalKeys.
type Gate interface {
	// Authed reports whether r carries a valid panel session.
	Authed(r *http.Request) bool
}

// requirePanel writes an error and returns false unless r may use the panel.
// State-changing requests must also come from the panel itself: behind a
// sign-in proxy every request counts as signed in, and a session cookie alone
// doesn't stop another site from submitting a form to this one.
func requirePanel(w http.ResponseWriter, r *http.Request, g Gate) bool {
	if !safeMethod(r.Method) && !fromPanel(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"bad_origin","message":"This request came from another site"}}`))
		return false
	}
	if g != nil && g.Authed(r) {
		return true
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":{"code":"panel_signed_out","message":"Sign in to the panel first"}}`))
	return false
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// fromPanel rejects the two shapes a cross-site request can take without a
// CORS preflight: an Origin naming another host, and a form-style body.
func fromPanel(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		host := strings.ToLower(u.Host)
		if host != strings.ToLower(r.Host) && host != strings.ToLower(firstValue(r.Header.Get("X-Forwarded-Host"))) {
			return false
		}
	} else if origin == "null" {
		return false
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mt, _, _ := mime.ParseMediaType(ct)
		switch mt {
		case "application/x-www-form-urlencoded", "multipart/form-data", "text/plain":
			return false
		}
	}
	return true
}

func firstValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
