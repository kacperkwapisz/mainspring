package app

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/kacperkwapisz/mainspring/internal/auth"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/session"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

const authBodyLimit = 64 << 10

// registerAuth wires the panel sign-in JSON API under /api/auth.
func registerAuth(mux *httpx.Mux, a *auth.Service) {
	mux.Handle(http.MethodGet, "/api/auth/state", func(w http.ResponseWriter, r *http.Request) {
		rp := a.RelyingParty(r)
		setup, err := a.SetupNeeded()
		if err != nil {
			authFail(w, err)
			return
		}
		all, err := a.Creds.Count()
		if err != nil {
			authFail(w, err)
			return
		}
		host, err := a.Creds.ListForRP(rp.ID)
		if err != nil {
			authFail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"mode":          a.Mode,
			"signedIn":      a.Authed(r),
			"setupNeeded":   setup,
			"secureContext": rp.SecureContext,
			"httpsURL":      a.HTTPSURL(),
			"rpID":          rp.ID,
			"requireKey":    a.RequireKey(),
			"keys":          all,
			"hostKeys":      len(host),
		})
	})

	mux.Handle(http.MethodPost, "/api/auth/setup/verify", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r, a) {
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		cookie, err := a.VerifySetupCode(r, body.Code)
		if err != nil {
			authFail(w, err)
			return
		}
		w.Header().Add("Set-Cookie", cookie)
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.Handle(http.MethodPost, "/api/auth/register/begin", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r, a) {
			return
		}
		allowed, fromSetup, err := a.CanRegister(r)
		if err != nil {
			authFail(w, err)
			return
		}
		if !allowed {
			if setup, _ := a.SetupNeeded(); setup {
				authError(w, http.StatusUnauthorized, "setup_code_required", "Enter the setup code from the hub's terminal first")
				return
			}
			requirePanel(w, r, nil)
			return
		}
		var body struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		opts, cookie, err := a.BeginRegistration(r, body.Kind, body.Name, fromSetup)
		if err != nil {
			authFail(w, err)
			return
		}
		w.Header().Add("Set-Cookie", cookie)
		httpx.JSON(w, http.StatusOK, map[string]any{"publicKey": opts})
	})

	mux.Handle(http.MethodPost, "/api/auth/register/finish", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r, a) {
			return
		}
		raw, ok := readRaw(w, r)
		if !ok {
			return
		}
		w.Header().Add("Set-Cookie", a.ClearCeremony(r))
		saved, fromSetup, err := a.FinishRegistration(r, raw)
		if err != nil {
			authFail(w, err)
			return
		}
		if fromSetup {
			cookie, err := a.IssueSession(r)
			if err != nil {
				authFail(w, err)
				return
			}
			w.Header().Add("Set-Cookie", cookie)
			w.Header().Add("Set-Cookie", session.ClearNamed(session.SetupCookieName, a.RelyingParty(r).Secure))
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"key": map[string]any{
				"id":        saved.ID,
				"name":      saved.Name,
				"kind":      saved.Kind,
				"createdAt": saved.CreatedAt,
			},
		})
	})

	mux.Handle(http.MethodPost, "/api/auth/login/begin", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r, a) {
			return
		}
		opts, cookie, err := a.BeginLogin(r)
		if err != nil {
			authFail(w, err)
			return
		}
		w.Header().Add("Set-Cookie", cookie)
		httpx.JSON(w, http.StatusOK, map[string]any{"publicKey": opts})
	})

	mux.Handle(http.MethodPost, "/api/auth/login/finish", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r, a) {
			return
		}
		raw, ok := readRaw(w, r)
		if !ok {
			return
		}
		w.Header().Add("Set-Cookie", a.ClearCeremony(r))
		if _, err := a.FinishLogin(r, raw); err != nil {
			authFail(w, err)
			return
		}
		cookie, err := a.IssueSession(r)
		if err != nil {
			authFail(w, err)
			return
		}
		w.Header().Add("Set-Cookie", cookie)
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.Handle(http.MethodPost, "/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r, a) {
			return
		}
		w.Header().Add("Set-Cookie", a.ClearSession(r))
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.Handle(http.MethodGet, "/api/auth/keys", func(w http.ResponseWriter, r *http.Request) {
		if !requirePanel(w, r, a) {
			return
		}
		rows, err := a.Creds.List()
		if err != nil {
			authFail(w, err)
			return
		}
		keys := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			keys = append(keys, keyJSON(row))
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"keys": keys})
	})

	mux.Handle(http.MethodPost, "/api/auth/keys/{id}/rename", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r, a) || !requirePanel(w, r, a) {
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		id := httpx.Param(r, "id")
		if err := a.RenameKey(id, body.Name); err != nil {
			authFail(w, err)
			return
		}
		row, _, err := a.Creds.Get(id)
		if err != nil {
			authFail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "key": keyJSON(row)})
	})

	mux.Handle(http.MethodDelete, "/api/auth/keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r, a) || !requirePanel(w, r, a) {
			return
		}
		if err := a.DeleteKey(httpx.Param(r, "id")); err != nil {
			authFail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.Handle(http.MethodPut, "/api/auth/require-key", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r, a) || !requirePanel(w, r, a) {
			return
		}
		var body struct {
			On *bool `json:"on"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		if body.On == nil {
			authError(w, http.StatusBadRequest, "on_required", "Send on: true or false")
			return
		}
		if err := a.SetRequireKey(r, *body.On); err != nil {
			authFail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "requireKey": a.RequireKey()})
	})
}

func keyJSON(row store.AuthCredential) map[string]any {
	var last any
	if row.LastUsedAt != nil {
		last = *row.LastUsedAt
	}
	return map[string]any{
		"id":         row.ID,
		"name":       row.Name,
		"kind":       row.Kind,
		"rpID":       row.RPID,
		"createdAt":  row.CreatedAt,
		"lastUsedAt": last,
	}
}

func authError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}

func authFail(w http.ResponseWriter, err error) {
	var e *auth.Error
	if errors.As(err, &e) {
		authError(w, e.Status, e.Code, e.Message)
		return
	}
	log.Printf("auth: %v", err)
	authError(w, http.StatusInternalServerError, "internal", "Something went wrong. Check the hub's log")
}

// sameOrigin refuses state-changing requests whose Origin header names
// another site. In proxy mode every request counts as signed in, so this is
// the CSRF guard.
func sameOrigin(w http.ResponseWriter, r *http.Request, a *auth.Service) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == a.RelyingParty(r).Origin {
		return true
	}
	authError(w, http.StatusForbidden, "bad_origin", "This request came from another site")
	return false
}

func readRaw(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, authBodyLimit+1))
	if err != nil {
		authError(w, http.StatusBadRequest, "body_unreadable", "Couldn't read the request")
		return nil, false
	}
	if len(raw) > authBodyLimit {
		authError(w, http.StatusRequestEntityTooLarge, "body_too_large", "The request is too large")
		return nil, false
	}
	return raw, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	raw, ok := readRaw(w, r)
	if !ok {
		return false
	}
	if len(raw) == 0 {
		return true
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		authError(w, http.StatusBadRequest, "body_invalid", "The request body isn't valid JSON")
		return false
	}
	return true
}
