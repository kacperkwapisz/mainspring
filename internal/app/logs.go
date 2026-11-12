package app

import (
	"net/http"
	"strconv"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
)

// registerLogs serves what the panel shows of Mainspring's own log and of
// failed requests. Neither ever holds tokens or keys: log lines are written
// without them, and failures keep only what the client was told.
func registerLogs(mux *httpx.Mux, hub *proxy.Hub, authed func(func(http.ResponseWriter, *http.Request)) http.HandlerFunc) {
	// GET /api/log?after=<seq>: the recent log, oldest first.
	mux.Handle(http.MethodGet, "/api/log", authed(func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		trace, _ := hub.Settings.Get(proxy.SettingLogTrace)
		if trace == "" {
			trace = httpx.TraceOff
		}
		httpx.JSON(w, 200, map[string]any{"entries": httpx.RecentLog(after), "trace": trace})
	}))
	// GET /api/errors?range=today|7d|14d: failed requests, newest first. 14 days by default.
	mux.Handle(http.MethodGet, "/api/errors", authed(func(w http.ResponseWriter, r *http.Request) {
		days := 14
		switch r.URL.Query().Get("range") {
		case "today":
			days = 1
		case "7d":
			days = 7
		}
		failures, err := hub.Usage.Failures(startOfDayAgo(proxy.Zone(hub), days-1), 200)
		if err != nil {
			apiError(w, 500, "errors_unavailable", err.Error())
			return
		}
		keyNames := keyNameMap(hub)
		accounts := map[string]string{}
		if creds, err := hub.Vault.List(); err == nil {
			for _, c := range creds {
				accounts[c.ID] = c.Label
			}
		}
		rows := make([]map[string]any, 0, len(failures))
		for _, f := range failures {
			rows = append(rows, map[string]any{
				"ts": f.TS, "provider": f.Provider, "provider_name": providerName(domain.ProviderID(f.Provider)),
				"model": f.Model, "inbound": f.Inbound, "latency_ms": f.LatencyMs,
				"status": f.Status, "message": f.Message,
				"key": keyNames[f.KeyID], "account": accounts[f.CredentialID],
			})
		}
		httpx.JSON(w, 200, map[string]any{"errors": rows})
	}))
}
