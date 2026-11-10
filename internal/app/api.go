package app

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/provider/openai"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
	"github.com/kacperkwapisz/mainspring/internal/sub"
)

// Version is set by main from the build.
var Version = "dev"

// Settings keys the panel edits.
const (
	SettingHubEnabled    = "hub.enabled"
	SettingHubKeyHash    = "hub.keyHash"
	SettingHubKeyCreated = "hub.keyCreatedAt"
)

// pooled providers have quota windows and live in the Pool groups.
var pooled = []domain.ProviderID{domain.ProviderClaude, domain.ProviderCodex}

type apiWindow struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Left     *float64 `json:"left"`
	ResetsAt *int64   `json:"resets_at"`
	PeriodMS int64    `json:"period_ms,omitempty"`
	Spent    bool     `json:"spent"`
	Soft     bool     `json:"soft,omitempty"`
}

type apiAccount struct {
	ID            string            `json:"id"`
	Provider      domain.ProviderID `json:"provider"`
	Label         string            `json:"label"`
	Email         string            `json:"email,omitempty"`
	Plan          string            `json:"plan,omitempty"`
	Kind          string            `json:"kind,omitempty"`
	Detail        string            `json:"detail,omitempty"`
	Status        string            `json:"status"` // next, active, cooling, benched, needs_sign_in
	Stale         bool              `json:"stale"`
	Order         int               `json:"order"` // 1-based routing order among usable accounts, 0 otherwise
	InRotation    bool              `json:"in_rotation"`
	CoolUntil     int64             `json:"cool_until,omitempty"`
	CoolWindow    string            `json:"cool_window,omitempty"`
	CoolSince     int64             `json:"cool_since,omitempty"`
	BenchedAt     int64             `json:"benched_at,omitempty"`
	SignInError   string            `json:"sign_in_error,omitempty"`
	SignInAt      int64             `json:"sign_in_at,omitempty"`
	Windows       []apiWindow       `json:"windows"`
	Short         *apiWindow        `json:"short,omitempty"`
	Long          *apiWindow        `json:"long,omitempty"`
	ObservedAt    int64             `json:"observed_at,omitempty"`
	Source        string            `json:"source,omitempty"`
	Sessions      int               `json:"sessions"`
	RequestsToday int64             `json:"requests_today"`
	USDToday      float64           `json:"usd_today"`
	CanCheck      bool              `json:"can_check"`
	ExpiresAt     *int64            `json:"expires_at,omitempty"`
	UpdatedAt     int64             `json:"updated_at"`
	CreatedAt     int64             `json:"created_at"`
	Models        int               `json:"models,omitempty"`
	BaseURL       string            `json:"base_url,omitempty"`
}

type poolView struct {
	now        int64
	staleMS    int64
	sessionsOn map[string]int
	today      map[string]store.UsageBreakdown
}

func startOfToday() int64 {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location()).UnixMilli()
}

func newPoolView(hub *proxy.Hub) poolView {
	v := poolView{now: time.Now().UnixMilli(), staleMS: staleMS(hub), sessionsOn: map[string]int{}, today: map[string]store.UsageBreakdown{}}
	for _, s := range hub.Router.LiveSessions() {
		v.sessionsOn[s.CredID]++
	}
	byCred, _ := hub.Usage.GroupFrom(startOfToday(), "credential_id")
	for _, b := range byCred {
		v.today[b.Key] = b
	}
	return v
}

func staleMS(hub *proxy.Hub) int64 {
	if v, ok := hub.Settings.Get(SettingStaleAfter); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return int64(n) * 60_000
		}
	}
	return 60 * 60_000
}

func toAPIWindow(w route.Window, now int64) apiWindow {
	out := apiWindow{ID: w.ID, Label: w.Label, ResetsAt: w.ResetsAt, PeriodMS: w.PeriodMS, Spent: w.Spent(now), Soft: w.Soft}
	if w.ResetsAt != nil && *w.ResetsAt <= now {
		// The window reset since we last looked: it's full, and when it next
		// resets isn't known until the account is read again.
		full := 100.0
		out.Left, out.ResetsAt = &full, nil
	} else if l := w.Left(); l >= 0 {
		out.Left = &l
	}
	return out
}

func (v poolView) account(hub *proxy.Hub, c domain.Credential, order int) apiAccount {
	st := hub.Router.Get(c.ID)
	a := apiAccount{
		ID: c.ID, Provider: c.Provider, Label: c.Label, Email: c.Tokens.Email, Plan: st.Plan,
		InRotation: !st.Benched, CoolWindow: st.CoolWindow, CoolSince: st.CoolSince,
		BenchedAt: st.BenchedAt, SignInError: st.SignInError, SignInAt: st.SignInAt,
		ObservedAt: st.ObservedAt, Source: st.Source, Sessions: v.sessionsOn[c.ID],
		CanCheck: sub.Supported(c.Provider), ExpiresAt: c.ExpiresAt, UpdatedAt: c.UpdatedAt, CreatedAt: c.CreatedAt,
		Windows: []apiWindow{},
	}
	if a.Plan == "" {
		if p, _ := c.Tokens.Extra["plan"].(string); p != "" {
			a.Plan = p
		}
	}
	if u, ok := v.today[c.ID]; ok {
		a.RequestsToday, a.USDToday = u.Requests, u.USD
	}
	for _, w := range st.Windows {
		a.Windows = append(a.Windows, toAPIWindow(w, v.now))
	}
	if w, ok := st.ShortWindow(v.now); ok {
		aw := toAPIWindow(w, v.now)
		a.Short = &aw
	}
	if w, ok := longest(st, v.now); ok && (a.Short == nil || w.ID != a.Short.ID) {
		aw := toAPIWindow(w, v.now)
		a.Long = &aw
	}
	switch {
	case st.NeedsSignIn:
		a.Status = "needs_sign_in"
	case st.Benched:
		a.Status = "benched"
	case !st.Usable(v.now):
		a.Status = "cooling"
		a.CoolUntil = st.BackAt(v.now)
		if a.CoolWindow == "" {
			for _, w := range st.Windows {
				if w.Spent(v.now) {
					a.CoolWindow = w.Label
				}
			}
		}
	case order == 1:
		a.Status = "next"
	default:
		a.Status = "active"
	}
	a.Order = order
	a.Stale = a.CanCheck && (st.ObservedAt == 0 || v.now-st.ObservedAt > v.staleMS)
	switch c.Provider {
	case domain.ProviderOpenAI:
		a.Kind = "openai"
		a.BaseURL, _ = c.Tokens.Extra["base_url"].(string)
		a.Detail = openai.SourceHost(c)
	case domain.ProviderOpenCode:
		a.Detail = "Service key"
	case domain.ProviderCursor:
		if k, _ := c.Tokens.Extra["kind"].(string); k == "api_key" {
			a.Detail = "API key"
		}
	}
	return a
}

// longest returns the window with the longest horizon, including spent ones.
func longest(st route.State, now int64) (route.Window, bool) {
	if w, ok := st.LongWindow(now); ok {
		return w, true
	}
	var best route.Window
	found := false
	for _, w := range st.Windows {
		if w.Soft {
			continue
		}
		if !found || w.PeriodMS > best.PeriodMS {
			best, found = w, true
		}
	}
	return best, found
}

type apiGroup struct {
	Provider domain.ProviderID `json:"provider"`
	Name     string            `json:"name"`
	Accounts []apiAccount      `json:"accounts"`
	NextUp   string            `json:"next_up,omitempty"`
	BackAt   int64             `json:"back_at,omitempty"` // when nothing can serve: first account back
	Sessions int               `json:"sessions"`
}

func providerName(p domain.ProviderID) string {
	switch p {
	case domain.ProviderClaude:
		return "Claude"
	case domain.ProviderCodex:
		return "Codex"
	case domain.ProviderGrok:
		return "Grok"
	case domain.ProviderCursor:
		return "Cursor"
	case domain.ProviderOpenCode:
		return "OpenCode Console"
	case domain.ProviderOpenAI:
		return "OpenAI-compatible"
	}
	return string(p)
}

func buildPool(hub *proxy.Hub) map[string]any {
	v := newPoolView(hub)
	creds, _ := hub.Vault.List()
	byProvider := map[domain.ProviderID][]domain.Credential{}
	for _, c := range creds {
		byProvider[c.Provider] = append(byProvider[c.Provider], c)
	}
	groups := []apiGroup{}
	for _, p := range pooled {
		list := byProvider[p]
		if len(list) == 0 {
			continue
		}
		g := groupFor(hub, v, p, list)
		groups = append(groups, g)
	}
	others := []apiAccount{}
	for _, p := range []domain.ProviderID{domain.ProviderCursor, domain.ProviderGrok, domain.ProviderOpenCode, domain.ProviderOpenAI} {
		for _, c := range byProvider[p] {
			others = append(others, v.account(hub, c, 0))
		}
	}
	totals, _ := hub.Usage.TotalsFrom(startOfToday())
	activity, _ := hub.Activity.Since(startOfToday(), 50)
	needs := 0
	for _, g := range groups {
		for _, a := range g.Accounts {
			if a.Status == "needs_sign_in" {
				needs++
			}
		}
	}
	return map[string]any{
		"now":       v.now,
		"groups":    groups,
		"others":    others,
		"sessions":  len(hub.Router.LiveSessions()),
		"requests":  totals.Requests,
		"activity":  activity,
		"stale_ms":  v.staleMS,
		"attention": needs,
		"empty":     len(creds) == 0,
	}
}

func groupFor(hub *proxy.Hub, v poolView, p domain.ProviderID, list []domain.Credential) apiGroup {
	order, backAt := hub.Router.Order(p, list, "")
	rank := map[string]int{}
	for i, c := range order {
		rank[c.Credential.ID] = i + 1
	}
	g := apiGroup{Provider: p, Name: providerName(p), Accounts: []apiAccount{}}
	for _, c := range list {
		a := v.account(hub, c, rank[c.ID])
		g.Sessions += a.Sessions
		g.Accounts = append(g.Accounts, a)
	}
	weight := map[string]int{"needs_sign_in": 0, "next": 1, "active": 2, "cooling": 3, "benched": 4}
	sort.SliceStable(g.Accounts, func(i, j int) bool {
		a, b := g.Accounts[i], g.Accounts[j]
		if weight[a.Status] != weight[b.Status] {
			return weight[a.Status] < weight[b.Status]
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		if a.Status == "cooling" {
			return a.CoolUntil < b.CoolUntil
		}
		return a.CreatedAt < b.CreatedAt
	})
	if len(order) > 0 {
		g.NextUp = order[0].Credential.ID
	} else {
		g.BackAt = backAt
	}
	return g
}

func registerAPI(mux *httpx.Mux, hub *proxy.Hub, gate Gate) {
	authed := func(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !requirePanel(w, r, gate) {
				return
			}
			h(w, r)
		}
	}
	mux.Handle(http.MethodGet, "/api/pool", authed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, buildPool(hub))
	}))
	mux.Handle(http.MethodGet, "/api/accounts/{id}", authed(func(w http.ResponseWriter, r *http.Request) {
		c := credOr404(w, hub, httpx.Param(r, "id"))
		if c == nil {
			return
		}
		httpx.JSON(w, 200, accountDetail(hub, *c))
	}))
	mux.Handle(http.MethodPost, "/api/accounts/{id}/rotation", authed(func(w http.ResponseWriter, r *http.Request) {
		c := credOr404(w, hub, httpx.Param(r, "id"))
		if c == nil {
			return
		}
		body, _ := httpx.ParseBody(r)
		on := jsonBool(body, "on")
		hub.Router.SetBenched(c.ID, !on)
		hub.Activity.Add(store.ActivityBenched, string(c.Provider), c.ID, map[string]any{"account": c.Label, "in_rotation": on})
		httpx.JSON(w, 200, accountDetail(hub, *c))
	}))
	mux.Handle(http.MethodPost, "/api/accounts/{id}/check", authed(func(w http.ResponseWriter, r *http.Request) {
		c := credOr404(w, hub, httpx.Param(r, "id"))
		if c == nil {
			return
		}
		if !sub.Supported(c.Provider) {
			apiError(w, 400, "no_usage_api", providerName(c.Provider)+" doesn't report limits")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		snap := proxy.CheckUsage(ctx, hub, *c)
		if !snap.OK {
			apiError(w, 502, "check_failed", "Couldn't check "+c.Label+": "+snap.Error)
			return
		}
		if fresh, err := hub.Vault.Get(c.ID); err == nil && fresh != nil {
			c = fresh
		}
		httpx.JSON(w, 200, accountDetail(hub, *c))
	}))
	mux.Handle(http.MethodPost, "/api/accounts/{id}/rename", authed(func(w http.ResponseWriter, r *http.Request) {
		c := credOr404(w, hub, httpx.Param(r, "id"))
		if c == nil {
			return
		}
		body, _ := httpx.ParseBody(r)
		name := strings.TrimSpace(httpx.JSONField(body, "name"))
		if name == "" || len(name) > 80 {
			apiError(w, 400, "name_invalid", "Give it a name of 1 to 80 characters")
			return
		}
		_ = hub.Vault.Rename(c.ID, name)
		c.Label = name
		httpx.JSON(w, 200, accountDetail(hub, *c))
	}))
	mux.Handle(http.MethodPost, "/api/accounts/{id}/reset-cooldown", authed(func(w http.ResponseWriter, r *http.Request) {
		c := credOr404(w, hub, httpx.Param(r, "id"))
		if c == nil {
			return
		}
		hub.Router.ClearCooldown(c.ID)
		httpx.JSON(w, 200, accountDetail(hub, *c))
	}))
	mux.Handle(http.MethodDelete, "/api/accounts/{id}", authed(func(w http.ResponseWriter, r *http.Request) {
		c := credOr404(w, hub, httpx.Param(r, "id"))
		if c == nil {
			return
		}
		if _, err := hub.Vault.Remove(c.ID); err != nil {
			apiError(w, 500, "remove_failed", err.Error())
			return
		}
		hub.Router.Forget(c.ID)
		hub.Activity.Add(store.ActivityRemoved, string(c.Provider), c.ID, map[string]any{"account": c.Label})
		httpx.JSON(w, 200, map[string]any{"ok": true})
	}))
	mux.Handle(http.MethodGet, "/api/events", authed(func(w http.ResponseWriter, r *http.Request) {
		streamChanges(w, r, hub.Live)
	}))
	mux.Handle(http.MethodGet, "/api/sessions", authed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, sessionsView(hub))
	}))
	mux.Handle(http.MethodGet, "/api/meter", authed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, meterView(hub, r.URL.Query().Get("range")))
	}))
	mux.Handle(http.MethodGet, "/api/keys", authed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, keysView(hub))
	}))
	mux.Handle(http.MethodPost, "/api/keys", authed(func(w http.ResponseWriter, r *http.Request) {
		createKey(w, r, hub)
	}))
	mux.Handle(http.MethodPost, "/api/keys/{id}/revoke", authed(func(w http.ResponseWriter, r *http.Request) {
		id := httpx.Param(r, "id")
		k, _ := hub.Keys.Get(id)
		if k == nil {
			apiError(w, 404, "not_found", "That key doesn't exist")
			return
		}
		_, _ = hub.Keys.Revoke(id)
		hub.Activity.Add(store.ActivityKeyRevoke, "", "", map[string]any{"key": k.Name})
		httpx.JSON(w, 200, map[string]any{"ok": true})
	}))
	mux.Handle(http.MethodGet, "/api/models", authed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, modelsView(hub))
	}))
	mux.Handle(http.MethodGet, "/api/settings", authed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, settingsView(hub))
	}))
	mux.Handle(http.MethodPut, "/api/settings", authed(func(w http.ResponseWriter, r *http.Request) {
		body, _ := httpx.ParseBody(r)
		if err := updateSettings(hub, body); err != nil {
			apiError(w, 400, "setting_invalid", err.Error())
			return
		}
		httpx.JSON(w, 200, settingsView(hub))
	}))
	mux.Handle(http.MethodPost, "/api/settings/hub-key", authed(func(w http.ResponseWriter, r *http.Request) {
		secret := newHubKey(hub)
		httpx.JSON(w, 200, map[string]any{"secret": secret, "settings": settingsView(hub)})
	}))
	mux.Handle(http.MethodDelete, "/api/settings/hub-key", authed(func(w http.ResponseWriter, r *http.Request) {
		_ = hub.Settings.Set(SettingHubKeyHash, "")
		_ = hub.Settings.Set(SettingHubEnabled, "0")
		httpx.JSON(w, 200, settingsView(hub))
	}))
}

func credOr404(w http.ResponseWriter, hub *proxy.Hub, id string) *domain.Credential {
	c, err := hub.Vault.Get(id)
	if err != nil || c == nil {
		apiError(w, 404, "not_found", "That account isn't in the pool")
		return nil
	}
	return c
}

func apiError(w http.ResponseWriter, status int, code, message string) {
	httpx.JSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}

func jsonBool(body any, key string) bool {
	m, _ := body.(map[string]any)
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return v == "1" || v == "true" || v == "on"
	case float64:
		return v != 0
	}
	return false
}

func accountDetail(hub *proxy.Hub, c domain.Credential) map[string]any {
	v := newPoolView(hub)
	rank := 0
	if p := c.Provider; p == domain.ProviderClaude || p == domain.ProviderCodex {
		list, _ := hub.Vault.List(p)
		order, _ := hub.Router.Order(p, list, "")
		for i, o := range order {
			if o.Credential.ID == c.ID {
				rank = i + 1
			}
		}
	}
	a := v.account(hub, c, rank)
	sessions := []map[string]any{}
	keyNames := keyNameMap(hub)
	for _, s := range hub.Router.LiveSessions() {
		if s.CredID == c.ID {
			sessions = append(sessions, sessionJSON(hub, s, keyNames))
		}
	}
	method := "OAuth"
	switch c.Provider {
	case domain.ProviderOpenAI, domain.ProviderOpenCode:
		method = "API key"
	case domain.ProviderCursor:
		if k, _ := c.Tokens.Extra["kind"].(string); k == "api_key" {
			method = "API key"
		}
	}
	return map[string]any{"account": a, "sessions": sessions, "sign_in": map[string]any{
		"method": method, "updated_at": c.UpdatedAt, "expires_at": c.ExpiresAt,
	}}
}

func keyNameMap(hub *proxy.Hub) map[string]string {
	keys, _ := hub.Keys.List()
	out := map[string]string{}
	for _, k := range keys {
		out[k.ID] = k.Name
	}
	return out
}

func sessionJSON(hub *proxy.Hub, s route.Session, keyNames map[string]string) map[string]any {
	label := func(id string) string {
		if id == "" {
			return ""
		}
		if c, _ := hub.Vault.Get(id); c != nil {
			return c.Label
		}
		return "removed account"
	}
	cacheShare := 0.0
	if s.InputTokens > 0 {
		cacheShare = float64(s.CacheRead) / float64(s.InputTokens)
	}
	return map[string]any{
		"id": s.ShortID, "provider": s.Provider, "account_id": s.CredID, "account": label(s.CredID),
		"key": keyNames[s.LocalKeyID], "client": s.Client, "model": s.Model, "transport": s.Transport,
		"started_at": s.StartedAt, "last_at": s.LastAt, "requests": s.Requests,
		"input_tokens": s.InputTokens, "cache_read_tokens": s.CacheRead, "cache_share": cacheShare,
		"moved_from": label(s.MovedFrom), "moved_at": s.MovedAt, "moved_reason": s.MovedReason,
	}
}

func sessionsView(hub *proxy.Hub) map[string]any {
	keyNames := keyNameMap(hub)
	list := []map[string]any{}
	var input, cached int64
	for _, s := range hub.Router.LiveSessions() {
		list = append(list, sessionJSON(hub, s, keyNames))
		input += s.InputTokens
		cached += s.CacheRead
	}
	share := 0.0
	if input > 0 {
		share = float64(cached) / float64(input)
	}
	return map[string]any{
		"sessions": list, "ttl_minutes": int(hub.Router.AffinityTTL().Minutes()), "cache_share": share,
	}
}
