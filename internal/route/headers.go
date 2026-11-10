package route

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Window is one quota window reported by a provider. Providers add, rename and
// drop windows over time, so nothing here assumes a fixed set: a window is an ID,
// a label, how much is used, when it resets and how long it lasts.
type Window struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	UsedPercent *float64 `json:"used_percent,omitempty"` // 0–100
	ResetsAt    *int64   `json:"resets_at,omitempty"`    // unix ms
	PeriodMS    int64    `json:"period_ms,omitempty"`    // length of the window when known
	Rejected    bool     `json:"rejected,omitempty"`     // provider says this window is spent
	// Soft windows (overage, extra-usage credit) never stop an account from serving.
	Soft bool `json:"soft,omitempty"`
}

// Left returns the reserve left in percent, or -1 when unknown.
func (w Window) Left() float64 {
	if w.UsedPercent == nil {
		return -1
	}
	left := 100 - *w.UsedPercent
	if left < 0 {
		return 0
	}
	return left
}

// Spent reports whether the window blocks the account at time now.
func (w Window) Spent(now int64) bool {
	if w.Soft {
		return false
	}
	if w.ResetsAt != nil && *w.ResetsAt <= now {
		return false
	}
	return w.Rejected || (w.UsedPercent != nil && *w.UsedPercent >= 100)
}

// horizon is how far a window reaches: its period when known, else its reset distance.
func (w Window) horizon(now int64) int64 {
	if w.PeriodMS > 0 {
		return w.PeriodMS
	}
	if w.ResetsAt != nil {
		return *w.ResetsAt - now
	}
	return 0
}

// SortWindows orders windows from shortest to longest horizon, soft ones last.
func SortWindows(ws []Window, now int64) {
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].Soft != ws[j].Soft {
			return !ws[i].Soft
		}
		return ws[i].horizon(now) < ws[j].horizon(now)
	})
}

const (
	hourMS           = 60 * 60 * 1000
	dayMS            = 24 * hourMS
	maxRecoverableMS = 7*dayMS + hourMS
)

var claudeLabels = map[string]string{
	"5h":      "5 hours",
	"7d":      "7 days",
	"7d_oi":   "7 days, OAuth apps",
	"7d_oa":   "7 days, OAuth apps",
	"7d_op":   "7 days, Opus",
	"overage": "Extra usage",
}

// ClaudeWindows reads every anthropic-ratelimit-unified-<window>-* header group.
// They are present on successful and rejected subscription responses alike.
func ClaudeWindows(h http.Header) []Window {
	if h == nil || h.Get("anthropic-ratelimit-unified-status") == "" {
		return nil
	}
	const prefix = "anthropic-ratelimit-unified-"
	keys := map[string]bool{}
	for name := range h {
		n := strings.ToLower(name)
		if !strings.HasPrefix(n, prefix) {
			continue
		}
		rest := n[len(prefix):]
		for _, suffix := range []string{"-utilization", "-reset", "-status"} {
			if strings.HasSuffix(rest, suffix) {
				key := strings.TrimSuffix(rest, suffix)
				// "overage-status" etc. belong to key "overage"; skip the bare unified fields.
				if key != "" && !strings.Contains(key, "-") {
					keys[key] = true
				}
			}
		}
	}
	var out []Window
	for key := range keys {
		p := prefix + key + "-"
		util, uok := parseFloat(h.Get(p + "utilization"))
		reset := parseUnix(h.Get(p + "reset"))
		status := strings.ToLower(h.Get(p + "status"))
		if !uok && reset == nil && status == "" {
			continue
		}
		win := Window{
			ID: "claude-" + key, Label: claudeLabel(key), ResetsAt: reset,
			PeriodMS: claudePeriod(key), Rejected: status == "rejected",
			Soft: strings.Contains(key, "overage"),
		}
		if uok {
			pct := util * 100
			if util > 1.5 {
				pct = util
			}
			pct = clamp(pct)
			win.UsedPercent = &pct
		}
		out = append(out, win)
	}
	SortWindows(out, time.Now().UnixMilli())
	return out
}

func claudeLabel(key string) string {
	if l, ok := claudeLabels[key]; ok {
		return l
	}
	if strings.HasPrefix(key, "7d_") {
		return "7 days, " + strings.ReplaceAll(strings.TrimPrefix(key, "7d_"), "_", " ")
	}
	return humanPeriod(claudePeriod(key), key)
}

func claudePeriod(key string) int64 {
	head := key
	if i := strings.IndexByte(key, '_'); i > 0 {
		head = key[:i]
	}
	if len(head) < 2 {
		return 0
	}
	n, err := strconv.Atoi(head[:len(head)-1])
	if err != nil || n <= 0 {
		return 0
	}
	switch head[len(head)-1] {
	case 'h':
		return int64(n) * hourMS
	case 'd':
		return int64(n) * dayMS
	}
	return 0
}

// CodexWindows reads every x-<limit>-primary-* and x-<limit>-secondary-* header
// group. The default limit is x-codex-*; model-specific ones use their own ids.
func CodexWindows(h http.Header) []Window {
	if h == nil {
		return nil
	}
	type group struct{ limit, side string }
	groups := map[group]bool{}
	for name := range h {
		n := strings.ToLower(name)
		if !strings.HasPrefix(n, "x-") {
			continue
		}
		for _, side := range []string{"-primary-", "-secondary-"} {
			if i := strings.Index(n, side); i > 2 {
				groups[group{limit: n[2:i], side: strings.Trim(side, "-")}] = true
			}
		}
	}
	var out []Window
	for g := range groups {
		p := "x-" + g.limit + "-" + g.side + "-"
		used, uok := parseFloat(h.Get(p + "used-percent"))
		minutes, _ := parseFloat(h.Get(p + "window-minutes"))
		reset := parseUnix(h.Get(p + "reset-at"))
		if reset == nil {
			if after, ok := parseFloat(h.Get(p + "reset-after-seconds")); ok && after >= 0 {
				ms := time.Now().UnixMilli() + int64(after*1000)
				reset = &ms
			}
		}
		if !uok && reset == nil {
			continue
		}
		period := int64(minutes * 60 * 1000)
		label := humanPeriod(period, g.side)
		id := g.limit + "-" + g.side
		if g.limit != "codex" {
			label = strings.ReplaceAll(strings.TrimPrefix(g.limit, "codex-"), "-", " ") + ", " + label
		}
		win := Window{ID: id, Label: label, ResetsAt: reset, PeriodMS: period}
		if uok {
			pct := clamp(used)
			win.UsedPercent = &pct
			win.Rejected = pct >= 100
		}
		out = append(out, win)
	}
	SortWindows(out, time.Now().UnixMilli())
	return out
}

func humanPeriod(ms int64, fallback string) string {
	switch {
	case ms <= 0:
		return strings.ReplaceAll(fallback, "_", " ")
	case ms%dayMS == 0 && ms >= dayMS:
		n := ms / dayMS
		if n == 1 {
			return "1 day"
		}
		if n >= 28 && n <= 31 {
			return "Monthly"
		}
		return strconv.FormatInt(n, 10) + " days"
	case ms%hourMS == 0:
		n := ms / hourMS
		if n == 1 {
			return "1 hour"
		}
		return strconv.FormatInt(n, 10) + " hours"
	default:
		return strconv.FormatInt(ms/60000, 10) + " min"
	}
}

// Limit describes why a 429 happened and until when the credential should rest.
type Limit struct {
	Until    int64  // unix ms; 0 when unknown
	WindowID string // the window that ran out, when known
	Label    string // its label, for the panel
	// Account is false when the rejection is not about this account's quota
	// (overage-only, or a plain per-minute rate limit).
	Account bool
}

// ClaudeLimit interprets a Claude 429. A spent subscription window cools the
// credential until that window resets.
func ClaudeLimit(h http.Header, now int64) Limit {
	var lim Limit
	for _, w := range ClaudeWindows(h) {
		if w.Soft || !w.Rejected || w.ResetsAt == nil || *w.ResetsAt <= lim.Until {
			continue
		}
		lim = Limit{Until: *w.ResetsAt, WindowID: w.ID, Label: w.Label, Account: true}
	}
	if lim.Until == 0 && strings.EqualFold(h.Get("anthropic-ratelimit-unified-status"), "rejected") {
		claim := strings.ToLower(h.Get("anthropic-ratelimit-unified-representative-claim"))
		if r := parseUnix(h.Get("anthropic-ratelimit-unified-reset")); r != nil && !strings.Contains(claim, "overage") {
			lim = Limit{Until: *r, WindowID: "claude-" + claudeClaimKey(claim), Label: claudeClaimLabel(claim), Account: true}
		}
	}
	if ra := RetryAfter(h, now); ra > lim.Until {
		lim.Until = ra
	}
	if lim.Until > 0 && lim.Until-now > maxRecoverableMS {
		lim.Until = 0
	}
	return lim
}

func claudeClaimKey(claim string) string {
	switch {
	case strings.HasPrefix(claim, "five_hour"):
		return "5h"
	case strings.HasPrefix(claim, "seven_day"):
		return "7d"
	}
	return claim
}

func claudeClaimLabel(claim string) string {
	if k := claudeClaimKey(claim); k != claim {
		return claudeLabel(k)
	}
	return strings.ReplaceAll(claim, "_", " ")
}

// CodexLimit interprets a Codex 429. A usage_limit_reached body wins over headers.
func CodexLimit(h http.Header, body []byte, now int64) Limit {
	var parsed struct {
		Error struct {
			Type               string  `json:"type"`
			ResetsAt           float64 `json:"resets_at"`
			ResetsInSeconds    float64 `json:"resets_in_seconds"`
			LimitWindowMinutes float64 `json:"limit_window_minutes"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &parsed)
	var lim Limit
	e := parsed.Error
	if e.Type == "usage_limit_reached" || e.ResetsAt > 0 || e.ResetsInSeconds > 0 {
		lim.Account = true
		switch {
		case e.ResetsAt > 0:
			lim.Until = unixToMS(e.ResetsAt)
		case e.ResetsInSeconds > 0:
			lim.Until = now + int64(e.ResetsInSeconds*1000)
		}
		if e.LimitWindowMinutes > 0 {
			lim.Label = humanPeriod(int64(e.LimitWindowMinutes*60*1000), "")
		}
	}
	for _, w := range CodexWindows(h) {
		if !w.Rejected || w.ResetsAt == nil {
			continue
		}
		if lim.Until == 0 || *w.ResetsAt >= lim.Until {
			lim.Until = *w.ResetsAt
		}
		lim.WindowID, lim.Account = w.ID, true
		if lim.Label == "" {
			lim.Label = w.Label
		}
	}
	if ra := RetryAfter(h, now); lim.Until == 0 && ra > 0 {
		lim.Until = ra
	}
	if lim.Until > 0 && lim.Until-now > maxRecoverableMS {
		lim.Until = 0
	}
	return lim
}

// RetryAfter parses a Retry-After header (seconds or HTTP date) into unix ms.
func RetryAfter(h http.Header, now int64) int64 {
	if h == nil {
		return 0
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 {
		return now + int64(n*1000)
	}
	if t, err := http.ParseTime(v); err == nil {
		return t.UnixMilli()
	}
	return 0
}

func parseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func parseUnix(s string) *int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		ms := unixToMS(f)
		return &ms
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, http.TimeFormat} {
		if t, err := time.Parse(layout, s); err == nil {
			ms := t.UnixMilli()
			return &ms
		}
	}
	return nil
}

func unixToMS(f float64) int64 {
	if f < 1e12 {
		return int64(f * 1000)
	}
	return int64(f)
}

func clamp(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
