package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/kacperkwapisz/mainspring/internal/auth"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/live"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
	"github.com/kacperkwapisz/mainspring/internal/tailnet"
)

// Settings owned by the tailnet device.
const (
	SettingTailnetOn       = "tailnet.enabled"
	SettingTailnetHostname = "tailnet.hostname"
)

// TopicTailnet tells open panels the tailnet status changed.
const TopicTailnet = "tailnet"

// startTailnet brings the device back after a restart; its keys are on disk,
// so it doesn't need signing in again.
func startTailnet(hub *proxy.Hub, m *tailnet.Manager) {
	if m == nil || !settingOn(hub, SettingTailnetOn, false) {
		return
	}
	name, _ := hub.Settings.Get(SettingTailnetHostname)
	if name == "" {
		name = "mainspring"
	}
	if err := m.Start(name, ""); err != nil {
		httpx.Logf("tailnet: couldn't start %s: %v", name, err)
	}
}

func tailnetJSON(m *tailnet.Manager, a *auth.Service) map[string]any {
	s := m.Status()
	out := map[string]any{
		"state": s.State, "hostname": s.Hostname, "auth_url": s.AuthURL, "tailnet": s.Tailnet,
		"user": s.User, "dns_name": s.DNSName, "ip": s.IP, "https": s.HTTPS,
		"cert_expires": s.CertExpires, "peers": s.Peers, "error": s.Error, "url": s.URL(),
	}
	if out["peers"] == nil {
		out["peers"] = []tailnet.Peer{}
	}
	// Security keys belong to the address they were added on. The panel says
	// whether this address has any, so it can offer to add the first one.
	if s.DNSName != "" && a != nil {
		if keys, err := a.Creds.ListForRP(s.DNSName); err == nil {
			out["keys"] = len(keys)
		}
	}
	return out
}

func registerTailnetAPI(mux *httpx.Mux, hub *proxy.Hub, gate Gate, a *auth.Service, m *tailnet.Manager) {
	authed := func(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !requirePanel(w, r, gate) {
				return
			}
			if m == nil {
				apiError(w, 404, "tailnet_unavailable", "This Mainspring can't join a tailnet")
				return
			}
			h(w, r)
		}
	}
	mux.Handle(http.MethodGet, "/api/tailnet", authed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, tailnetJSON(m, a))
	}))
	mux.Handle(http.MethodPost, "/api/tailnet/start", authed(func(w http.ResponseWriter, r *http.Request) {
		body, _ := httpx.ParseBody(r)
		name := strings.ToLower(strings.TrimSpace(httpx.JSONField(body, "hostname")))
		if name == "" {
			name = "mainspring"
		}
		if !tailnet.ValidHostname(name) {
			apiError(w, 400, "hostname_invalid", "Use lowercase letters, numbers and dashes, up to 63 characters")
			return
		}
		if err := m.Start(name, strings.TrimSpace(httpx.JSONField(body, "auth_key"))); err != nil {
			apiError(w, 502, "tailnet_start_failed", "Couldn't start the device: "+err.Error())
			return
		}
		_ = hub.Settings.Set(SettingTailnetHostname, name)
		_ = hub.Settings.Set(SettingTailnetOn, "1")
		httpx.JSON(w, 200, tailnetJSON(m, a))
	}))
	mux.Handle(http.MethodPost, "/api/tailnet/check", authed(func(w http.ResponseWriter, r *http.Request) {
		m.Recheck()
		httpx.JSON(w, 200, tailnetJSON(m, a))
	}))
	mux.Handle(http.MethodPost, "/api/tailnet/cancel", authed(func(w http.ResponseWriter, r *http.Request) {
		// Leaving before the device joined: forget it, there's nothing to sign out.
		if s := m.Status(); s.State == tailnet.Starting || s.State == tailnet.NeedsLogin {
			_ = m.Forget()
			_ = hub.Settings.Set(SettingTailnetOn, "0")
		}
		httpx.JSON(w, 200, tailnetJSON(m, a))
	}))
	mux.Handle(http.MethodDelete, "/api/tailnet", authed(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err := m.Remove(ctx); err != nil {
			apiError(w, 500, "tailnet_remove_failed", "Couldn't remove the device: "+err.Error())
			return
		}
		_ = hub.Settings.Set(SettingTailnetOn, "0")
		httpx.JSON(w, 200, tailnetJSON(m, a))
	}))
	// open returns the address to open. When no security key exists yet, it
	// carries a one-time setup code, so the first key can be added there:
	// the tailnet address skips the sign-in proxy that vouches for this request.
	mux.Handle(http.MethodPost, "/api/tailnet/open", authed(func(w http.ResponseWriter, r *http.Request) {
		s := m.Status()
		if s.URL() == "" {
			apiError(w, 409, "tailnet_not_joined", "Mainspring isn't on your tailnet yet")
			return
		}
		url := s.URL() + "/"
		if n, err := a.Creds.Count(); err == nil && n == 0 {
			code, err := a.NewSetupCode()
			if err != nil {
				apiError(w, 500, "setup_code_failed", "Couldn't prepare the first sign-in")
				return
			}
			url += "?setup=" + code
		}
		httpx.JSON(w, 200, map[string]any{"url": url})
	}))
	mux.Handle(http.MethodGet, "/api/tailnet/qr.svg", authed(func(w http.ResponseWriter, r *http.Request) {
		s := m.Status()
		if s.AuthURL == "" {
			http.NotFound(w, r)
			return
		}
		code, err := qr.Encode(s.AuthURL, qr.M)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(qrSVG(code)))
	}))
}

// qrSVG draws a QR code dark on white with the standard four-module margin.
func qrSVG(code *qr.Code) string {
	n := code.Size
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="-4 -4 %d %d" shape-rendering="crispEdges"><rect x="-4" y="-4" width="%d" height="%d" fill="#fff"/><path fill="#111" d="`, n+8, n+8, n+8, n+8)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x, y)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}

func publishTailnet(hub *proxy.Hub) func() {
	return func() { hub.Live.Publish(TopicTailnet, live.Settings) }
}
