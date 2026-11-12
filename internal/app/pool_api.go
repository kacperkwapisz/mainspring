package app

import (
	"net/http"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
)

// GET /v1/pool shows clients holding a Mainspring key the pool: each
// account's status and limits, which one goes next, and the key's own live
// sessions. The pi extension's /mainspring overlay reads it. Off with the
// Share pool with keys setting.
func registerPoolAPI(mux *httpx.Mux, hub *proxy.Hub) {
	mux.Handle(http.MethodGet, "/v1/pool", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, hub)
		if key == nil {
			writeUnauthorized(w, r, string(domain.InboundOpenAIChat))
			return
		}
		if !proxy.SharePool(hub) {
			httpx.JSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{
				"type": "invalid_request_error", "message": "This Mainspring doesn't share its pool with keys. Turn on Share pool with keys under Settings → Keys",
			}})
			return
		}
		pool := buildPool(hub, viewerZone(r))
		type window struct {
			Label    string   `json:"label"`
			Left     *float64 `json:"left"`
			ResetsAt *int64   `json:"resets_at,omitempty"`
			PeriodMS int64    `json:"period_ms,omitempty"`
		}
		type account struct {
			Label     string   `json:"label"`
			Plan      string   `json:"plan,omitempty"`
			Status    string   `json:"status"`
			Stale     bool     `json:"stale,omitempty"`
			CoolUntil int64    `json:"cool_until,omitempty"`
			Sessions  int      `json:"sessions"`
			Windows   []window `json:"windows"`
		}
		convert := func(a apiAccount) account {
			out := account{Label: a.Label, Plan: a.Plan, Status: a.Status, Stale: a.Stale, CoolUntil: a.CoolUntil, Sessions: a.Sessions, Windows: []window{}}
			for _, win := range a.Windows {
				if win.Soft {
					continue
				}
				out.Windows = append(out.Windows, window{Label: win.Label, Left: win.Left, ResetsAt: win.ResetsAt, PeriodMS: win.PeriodMS})
			}
			return out
		}
		var groups []map[string]any
		for _, g := range pool["groups"].([]apiGroup) {
			var accounts []account
			next := ""
			for _, a := range g.Accounts {
				accounts = append(accounts, convert(a))
				if a.ID == g.NextUp {
					next = a.Label
				}
			}
			groups = append(groups, map[string]any{"provider": g.Provider, "name": g.Name, "next_up": next, "back_at": g.BackAt, "accounts": accounts})
		}
		var others []map[string]any
		for _, a := range pool["others"].([]apiAccount) {
			others = append(others, map[string]any{"provider": a.Provider, "name": providerName(a.Provider), "label": a.Label, "status": a.Status})
		}
		var sessions []map[string]any
		for _, s := range hub.Router.LiveSessions() {
			if s.LocalKeyID != key.ID {
				continue
			}
			label := ""
			if c, err := hub.Vault.Get(s.CredID); err == nil && c != nil {
				label = c.Label
			}
			sessions = append(sessions, map[string]any{"provider": s.Provider, "account": label, "model": s.Model, "client": s.Client, "requests": s.Requests, "last_at": s.LastAt})
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"now": pool["now"], "groups": nonNil(groups), "others": nonNil(others), "sessions": nonNil(sessions)})
	})
}

func nonNil(v []map[string]any) []map[string]any {
	if v == nil {
		return []map[string]any{}
	}
	return v
}
