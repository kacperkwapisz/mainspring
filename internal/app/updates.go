package app

import (
	"context"
	"net/http"
	"time"

	mainspring "github.com/kacperkwapisz/mainspring"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/live"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
	"github.com/kacperkwapisz/mainspring/internal/update"
)

// SettingUpdateCheck turns the GitHub release check on (default) or off.
const SettingUpdateCheck = "updates.check"

// UpdateEvery is how often Mainspring asks GitHub for a new release.
const UpdateEvery = 6 * time.Hour

func updateStatus(hub *proxy.Hub) update.Status {
	return hub.Updates.Status(settingOn(hub, SettingUpdateCheck, true))
}

// CheckForUpdates asks GitHub when the setting is on and tells open panels.
func CheckForUpdates(ctx context.Context, hub *proxy.Hub, force bool) update.Status {
	if hub.Updates == nil {
		return update.Status{}
	}
	if force || settingOn(hub, SettingUpdateCheck, true) {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_ = hub.Updates.Check(cctx)
		cancel()
		hub.Live.Publish(live.Updates)
	}
	return updateStatus(hub)
}

func registerUpdatesAPI(mux *httpx.Mux, hub *proxy.Hub, gate Gate) {
	authed := func(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !requirePanel(w, r, gate) {
				return
			}
			h(w, r)
		}
	}
	mux.Handle(http.MethodGet, "/api/updates", authed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, updateStatus(hub))
	}))
	mux.Handle(http.MethodPost, "/api/updates/check", authed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, CheckForUpdates(r.Context(), hub, true))
	}))
	mux.Handle(http.MethodGet, "/api/changelog", authed(func(w http.ResponseWriter, r *http.Request) {
		releases := update.ParseChangelog(mainspring.Changelog)
		if len(releases) > 20 {
			releases = releases[:20]
		}
		httpx.JSON(w, 200, map[string]any{"current": Version, "releases": releases})
	}))
}
