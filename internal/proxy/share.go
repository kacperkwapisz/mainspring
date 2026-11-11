package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

// SettingSharePool lets clients holding a Mainspring key see which account
// served them and the pool's limits (response headers and GET /v1/pool).
// On by default; turning it off keeps account emails to the panel.
const SettingSharePool = "keys.sharePool"

// SharePool reports whether keys may see accounts and limits.
func SharePool(hub *Hub) bool {
	if hub == nil || hub.Settings == nil {
		return false
	}
	v, ok := hub.Settings.Get(SettingSharePool)
	return !ok || v == "" || v == "1"
}

// limitHeader is one window in X-Mainspring-Limits.
type limitHeader struct {
	Label    string  `json:"label"`
	Left     float64 `json:"left"`                // percent left, 0–100
	ResetsAt int64   `json:"resets_at,omitempty"` // unix ms
}

// accountHeaders tells the client which account served it, its limits, and
// why the session moved when it did. Values are kept to printable ASCII.
func accountHeaders(hub *Hub, h http.Header, provider domain.ProviderID, cred domain.Credential, movedFrom, movedWhy string) http.Header {
	if !SharePool(hub) {
		return h
	}
	if h == nil {
		h = http.Header{}
	}
	h.Set("X-Mainspring-Account", headerSafe(cred.Label))
	h.Set("X-Mainspring-Provider", string(provider))
	now := time.Now().UnixMilli()
	var limits []limitHeader
	for _, w := range hub.router().Get(cred.ID).Windows {
		left := w.Left()
		if left < 0 || w.Soft {
			continue
		}
		l := limitHeader{Label: headerSafe(w.Label), Left: left}
		if w.ResetsAt != nil && *w.ResetsAt > now {
			l.ResetsAt = *w.ResetsAt
		} else if w.ResetsAt != nil {
			l.Left = 100 // the window reset since it was read
		}
		limits = append(limits, l)
	}
	if len(limits) > 0 {
		raw, _ := json.Marshal(limits)
		h.Set("X-Mainspring-Limits", string(raw))
	}
	if movedFrom != "" {
		if from, err := hub.Vault.Get(movedFrom); err == nil && from != nil {
			h.Set("X-Mainspring-Moved-From", headerSafe(from.Label))
			h.Set("X-Mainspring-Moved-Reason", headerSafe(movedWhy))
		}
	}
	return h
}

func headerSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return '?'
		}
		return r
	}, s)
}
