package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	fcrypto "github.com/kacperkwapisz/mainspring/internal/crypto"
	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

const dayMillis = 24 * 60 * 60 * 1000

// ---- Meter ----

func meterView(hub *proxy.Hub, rng string) map[string]any {
	days := 1
	switch rng {
	case "7d":
		days = 7
	case "14d":
		days = 14
	default:
		rng = "today"
	}
	from := startOfToday() - int64(days-1)*dayMillis
	byProvider, _ := hub.Usage.GroupFrom(from, "provider")
	var input, output, cacheRead, cacheWrite, requests int64
	var usd float64
	for _, b := range byProvider {
		p := domain.ProviderID(b.Key)
		input += proxy.InputTokens(p, b.PromptTokens, b.CacheReadTokens, b.CacheWriteTokens)
		output += b.CompletionTokens
		cacheRead += b.CacheReadTokens
		cacheWrite += b.CacheWriteTokens
		requests += b.Requests
		usd += b.USD
	}
	// Cost by model, grouped by provider.
	byModel, _ := hub.Usage.GroupFrom(from, "provider_model")
	type modelRow struct {
		Model    string  `json:"model"`
		USD      float64 `json:"usd"`
		Requests int64   `json:"requests"`
		Tokens   int64   `json:"tokens"`
	}
	type providerRow struct {
		Provider domain.ProviderID `json:"provider"`
		Name     string            `json:"name"`
		USD      float64           `json:"usd"`
		Requests int64             `json:"requests"`
		Models   []modelRow        `json:"models"`
	}
	prov := map[domain.ProviderID]*providerRow{}
	for _, b := range byModel {
		parts := strings.SplitN(b.Key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		p := domain.ProviderID(parts[0])
		row := prov[p]
		if row == nil {
			row = &providerRow{Provider: p, Name: providerName(p), Models: []modelRow{}}
			prov[p] = row
		}
		tokens := proxy.InputTokens(p, b.PromptTokens, b.CacheReadTokens, b.CacheWriteTokens) + b.CompletionTokens
		row.Models = append(row.Models, modelRow{Model: parts[1], USD: b.USD, Requests: b.Requests, Tokens: tokens})
		row.USD += b.USD
		row.Requests += b.Requests
	}
	costs := []providerRow{}
	for _, r := range prov {
		sort.Slice(r.Models, func(i, j int) bool { return r.Models[i].USD > r.Models[j].USD })
		costs = append(costs, *r)
	}
	sort.Slice(costs, func(i, j int) bool { return costs[i].USD > costs[j].USD })

	// Requests by account, grouped by provider.
	byCred, _ := hub.Usage.GroupFrom(from, "credential_id")
	creds, _ := hub.Vault.List()
	credByID := map[string]domain.Credential{}
	for _, c := range creds {
		credByID[c.ID] = c
	}
	type accountRow struct {
		ID       string  `json:"id"`
		Label    string  `json:"label"`
		Detail   string  `json:"detail,omitempty"`
		Requests int64   `json:"requests"`
		USD      float64 `json:"usd"`
	}
	type accountGroup struct {
		Provider domain.ProviderID `json:"provider"`
		Name     string            `json:"name"`
		Requests int64             `json:"requests"`
		Accounts []accountRow      `json:"accounts"`
	}
	ag := map[domain.ProviderID]*accountGroup{}
	for _, b := range byCred {
		c, ok := credByID[b.Key]
		if !ok {
			continue
		}
		g := ag[c.Provider]
		if g == nil {
			g = &accountGroup{Provider: c.Provider, Name: providerName(c.Provider), Accounts: []accountRow{}}
			ag[c.Provider] = g
		}
		g.Accounts = append(g.Accounts, accountRow{ID: c.ID, Label: c.Label, Requests: b.Requests, USD: b.USD})
		g.Requests += b.Requests
	}
	accounts := []accountGroup{}
	for _, g := range ag {
		sort.Slice(g.Accounts, func(i, j int) bool { return g.Accounts[i].Requests > g.Accounts[j].Requests })
		accounts = append(accounts, *g)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Requests > accounts[j].Requests })

	// Web tools: searches and page reads through /mcp, by provider.
	type webRow struct {
		Provider domain.ProviderID `json:"provider"`
		Name     string            `json:"name"`
		Searches int64             `json:"searches"`
		Fetches  int64             `json:"fetches"`
		Errors   int64             `json:"errors"`
		Tokens   int64             `json:"tokens"`
		USD      float64           `json:"usd"`
	}
	web := map[domain.ProviderID]*webRow{}
	var webTotal webRow
	byInbound, _ := hub.Usage.GroupFrom(from, "inbound_provider")
	for _, b := range byInbound {
		parts := strings.SplitN(b.Key, "|", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "mcp.") {
			continue
		}
		p := domain.ProviderID(parts[1])
		row := web[p]
		if row == nil {
			row = &webRow{Provider: p, Name: providerName(p)}
			web[p] = row
		}
		ok := b.Requests - b.Errors
		tokens := proxy.InputTokens(p, b.PromptTokens, b.CacheReadTokens, b.CacheWriteTokens) + b.CompletionTokens
		for _, r := range []*webRow{row, &webTotal} {
			if parts[0] == "mcp.web_search" {
				r.Searches += ok
			} else {
				r.Fetches += ok
			}
			r.Errors += b.Errors
			r.Tokens += tokens
			r.USD += b.USD
		}
	}
	webRows := []webRow{}
	for _, r := range web {
		webRows = append(webRows, *r)
	}
	sort.Slice(webRows, func(i, j int) bool {
		return webRows[i].Searches+webRows[i].Fetches > webRows[j].Searches+webRows[j].Fetches
	})

	daily, _ := hub.Usage.Daily(14)
	type dayRow struct {
		Day    string  `json:"day"`
		Tokens int64   `json:"tokens"`
		USD    float64 `json:"usd"`
	}
	chart := []dayRow{}
	for _, d := range daily {
		chart = append(chart, dayRow{Day: d.Day, Tokens: d.PromptTokens + d.CompletionTokens + d.CacheReadTokens + d.CacheWriteTokens, USD: d.USD})
	}
	return map[string]any{
		"range": rng, "from": from, "requests": requests, "usd": usd,
		"tokens": input + output, "input": input, "output": output,
		"cache_read": cacheRead, "cache_write": cacheWrite,
		"uncached": maxInt(0, input-cacheRead-cacheWrite),
		"keys":     countKeys(hub), "accounts": len(byCred),
		"costs": costs, "by_account": accounts, "daily": chart,
		"web":         map[string]any{"searches": webTotal.Searches, "fetches": webTotal.Fetches, "errors": webTotal.Errors, "tokens": webTotal.Tokens, "usd": webTotal.USD, "by_provider": webRows},
		"first_event": hub.Usage.FirstEvent(),
	}
}

func maxInt(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func countKeys(hub *proxy.Hub) int {
	keys, _ := hub.Keys.List()
	n := 0
	for _, k := range keys {
		if !k.Revoked {
			n++
		}
	}
	return n
}

// ---- Keys ----

func keysView(hub *proxy.Hub) map[string]any {
	keys, _ := hub.Keys.List()
	lastUsed, _ := hub.Usage.LastUsed()
	byKey, _ := hub.Usage.GroupFrom(startOfToday(), "key_id")
	used := map[string]int64{}
	for _, b := range byKey {
		used[b.Key] = b.PromptTokens + b.CompletionTokens
	}
	sessions := map[string]int{}
	for _, s := range hub.Router.LiveSessions() {
		sessions[s.LocalKeyID]++
	}
	out := []map[string]any{}
	var totalUsed int64
	for _, k := range keys {
		if k.Revoked {
			continue
		}
		totalUsed += used[k.ID]
		out = append(out, map[string]any{
			"id": k.ID, "name": k.Name, "prefix": k.Prefix, "providers": k.Providers, "models": k.Models,
			"daily_cap": k.DailyCap, "used_today": used[k.ID], "sessions": sessions[k.ID],
			"last_used_at": lastUsed[k.ID], "created_at": k.CreatedAt,
		})
	}
	return map[string]any{"keys": out, "used_today": totalUsed}
}

func createKey(w http.ResponseWriter, r *http.Request, hub *proxy.Hub) {
	body, _ := httpx.ParseBody(r)
	m, _ := body.(map[string]any)
	name := strings.TrimSpace(httpx.JSONField(body, "name"))
	if name == "" || len(name) > 64 {
		apiError(w, 400, "name_invalid", "Name it after the machine or tool that will use it")
		return
	}
	existing, _ := hub.Keys.List()
	for _, k := range existing {
		if !k.Revoked && strings.EqualFold(k.Name, name) {
			apiError(w, 400, "name_taken", "A key called "+k.Name+" already exists")
			return
		}
	}
	var providers []domain.ProviderID
	if arr, ok := m["providers"].([]any); ok {
		for _, v := range arr {
			s, _ := v.(string)
			if domain.IsProviderID(s) {
				providers = append(providers, domain.ProviderID(s))
			}
		}
	}
	var models []string
	switch v := m["models"].(type) {
	case string:
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				models = append(models, p)
			}
		}
	case []any:
		for _, part := range v {
			if s, _ := part.(string); strings.TrimSpace(s) != "" {
				models = append(models, strings.TrimSpace(s))
			}
		}
	}
	var cap *int64
	if raw := strings.TrimSpace(fmt.Sprint(m["daily_cap"])); raw != "" && raw != "<nil>" {
		n, err := parseTokenCount(raw)
		if err != nil || n <= 0 {
			apiError(w, 400, "cap_invalid", "Write the cap as a number of tokens, like 20M or 500k")
			return
		}
		cap = &n
	}
	created, err := hub.Keys.Create(name, providers, models, cap)
	if err != nil {
		apiError(w, 500, "create_failed", err.Error())
		return
	}
	hub.Activity.Add(store.ActivityKeyAdded, "", "", map[string]any{"key": name})
	httpx.JSON(w, 200, map[string]any{
		"secret": created.Secret,
		"key": map[string]any{"id": created.Key.ID, "name": created.Key.Name, "prefix": created.Key.Prefix,
			"providers": providers, "models": models, "daily_cap": cap, "created_at": created.Key.CreatedAt},
	})
}

// parseTokenCount reads "20M", "500k", "1.5m" or a plain number.
func parseTokenCount(s string) (int64, error) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), ",", ""))
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "m"):
		mult, s = 1e6, strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "k"):
		mult, s = 1e3, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "b"):
		mult, s = 1e9, strings.TrimSuffix(s, "b")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	return int64(f * mult), nil
}

// ---- Models ----

func modelsView(hub *proxy.Hub) map[string]any {
	models := proxy.ListModels(hub)
	type row struct {
		ID       string   `json:"id"`
		ServedBy string   `json:"served_by"`
		Fallback string   `json:"fallback,omitempty"`
		Context  int      `json:"context_length,omitempty"`
		Efforts  []string `json:"efforts,omitempty"`
		Input    []string `json:"input,omitempty"`
		Name     string   `json:"name,omitempty"`
		Enabled  bool     `json:"enabled"`
	}
	off := proxy.DisabledModels(hub)
	rows := []row{}
	for _, m := range models {
		served, fallback := proxy.ServedBy(hub, m.ID)
		names := []string{}
		for _, p := range served {
			names = append(names, providerName(p))
		}
		fb := ""
		if fallback != "" {
			fb = providerName(fallback)
		}
		r := row{ID: m.ID, Context: m.ContextLength, Efforts: m.Efforts, Input: m.Input, Name: m.Name, Enabled: !off[m.ID]}
		if len(names) > 0 {
			r.ServedBy = names[0]
		}
		r.Fallback = fb
		rows = append(rows, r)
	}
	return map[string]any{"models": rows}
}

// ---- Settings ----

func settingOn(hub *proxy.Hub, key string, def bool) bool {
	v, ok := hub.Settings.Get(key)
	if !ok || v == "" {
		return def
	}
	return v == "1"
}

func settingInt(hub *proxy.Hub, key string, def int) int {
	if v, ok := hub.Settings.Get(key); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func settingsView(hub *proxy.Hub) map[string]any {
	trace, _ := hub.Settings.Get(proxy.SettingLogTrace)
	if trace == "" {
		trace = httpx.TraceOff
	}
	hash, _ := hub.Settings.Get(SettingHubKeyHash)
	created, _ := hub.Settings.Get(SettingHubKeyCreated)
	createdAt, _ := strconv.ParseInt(created, 10, 64)
	return map[string]any{
		"affinity_minutes":   settingInt(hub, SettingAffinityTTL, 60),
		"stale_minutes":      settingInt(hub, SettingStaleAfter, 60),
		"cursor_prefix":      settingOn(hub, proxy.SettingCursorPrefix, false),
		"cursor_list_fast":   settingOn(hub, proxy.SettingCursorListFast, true),
		"grok_failover":      settingOn(hub, proxy.SettingCursorGrokFailover, false),
		"trace":              trace,
		"websockets":         settingOn(hub, SettingWSEnabled, true),
		"hub_enabled":        settingOn(hub, SettingHubEnabled, false),
		"hub_key":            hash != "",
		"hub_key_created_at": createdAt,
		"version":            Version,
		"update_check":       settingOn(hub, SettingUpdateCheck, true),
		"web_tools":          settingOn(hub, SettingWebTools, true),
		"share_pool":         proxy.SharePool(hub),
	}
}

func updateSettings(hub *proxy.Hub, body any) error {
	m, _ := body.(map[string]any)
	setBool := func(field, key string) {
		if _, ok := m[field]; ok {
			v := "0"
			if jsonBool(m, field) {
				v = "1"
			}
			_ = hub.Settings.Set(key, v)
		}
	}
	setMinutes := func(field, key string) error {
		raw, ok := m[field]
		if !ok {
			return nil
		}
		n, _ := raw.(float64)
		if n < 5 || n > 24*60 {
			return fmt.Errorf("pick between 5 minutes and 24 hours")
		}
		return hub.Settings.Set(key, strconv.Itoa(int(n)))
	}
	if err := setMinutes("affinity_minutes", SettingAffinityTTL); err != nil {
		return err
	}
	if err := setMinutes("stale_minutes", SettingStaleAfter); err != nil {
		return err
	}
	applyRoutingSettings(hub.Router, hub.Settings)
	setBool("cursor_prefix", proxy.SettingCursorPrefix)
	setBool("cursor_list_fast", proxy.SettingCursorListFast)
	setBool("grok_failover", proxy.SettingCursorGrokFailover)
	setBool("websockets", SettingWSEnabled)
	setBool("update_check", SettingUpdateCheck)
	setBool("web_tools", SettingWebTools)
	setBool("share_pool", proxy.SettingSharePool)
	if _, ok := m["hub_enabled"]; ok {
		if jsonBool(m, "hub_enabled") {
			if h, _ := hub.Settings.Get(SettingHubKeyHash); h == "" {
				return fmt.Errorf("create a management key first")
			}
		}
		setBool("hub_enabled", SettingHubEnabled)
	}
	if t, ok := m["trace"].(string); ok {
		switch t {
		case httpx.TraceOff, httpx.TraceErrors, httpx.TraceAlways:
			_ = hub.Settings.Set(proxy.SettingLogTrace, t)
			httpx.SetTraceMode(t)
		default:
			return fmt.Errorf("trace must be off, errors or always")
		}
	}
	return nil
}

// newHubKey creates the management key dashboards use. Only its hash is kept.
func newHubKey(hub *proxy.Hub) string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	secret := "ms-hub-" + hex.EncodeToString(buf)
	_ = hub.Settings.Set(SettingHubKeyHash, fcrypto.SHA256Hex(secret))
	_ = hub.Settings.Set(SettingHubKeyCreated, strconv.FormatInt(time.Now().UnixMilli(), 10))
	_ = hub.Settings.Set(SettingHubEnabled, "1")
	return secret
}
