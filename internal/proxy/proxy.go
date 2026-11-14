package proxy

import (
	"context"
	"fmt"
	"github.com/kacperkwapisz/mainspring/internal/live"
	"github.com/kacperkwapisz/mainspring/internal/status"
	"github.com/kacperkwapisz/mainspring/internal/update"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/env"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/provider/cursor"
	"github.com/kacperkwapisz/mainspring/internal/provider/openai"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
	"github.com/kacperkwapisz/mainspring/internal/translate"
)

type liveModels interface {
	ModelsFor(context.Context, domain.Credential) []domain.ModelInfo
}

const (
	SettingCursorPrefix       = "cursor.prefix"
	SettingCursorGrokFailover = "cursor.grokFailover"
	SettingCursorListFast     = "cursor.listFast"
	SettingLogTrace           = httpx.SettingLogTrace
)

type Hub struct {
	Env       *env.Env
	Vault     *store.Vault
	Keys      *store.KeyStore
	Usage     *store.UsageStore
	Prices    *store.PriceStore
	Executors map[domain.ProviderID]provider.Executor
	Settings  *store.SettingsStore
	Router    *route.Engine
	Activity  *store.ActivityStore
	Live      *live.Bus // tells open panels what changed
	Updates   *update.Checker
	Status    *status.Watcher // open incidents on providers' status pages
}

type Request struct {
	Inbound        domain.InboundFormat
	Body           any
	Key            domain.LocalKey
	Stream         bool
	CountTokens    bool
	Compact        bool
	InboundHeaders map[string]string
	// Transport is "http" or "websocket", shown on the Sessions screen.
	Transport string
}

type Result struct {
	OK     bool
	Status int
	Body   any
	Stream <-chan string
	// Header carries response headers for the client (Retry-After, Codex turn state).
	Header http.Header
}

func Proxy(ctx context.Context, hub *Hub, req Request) (Result, error) {
	started := time.Now().UnixMilli()
	syncTraceMode(hub)
	if httpx.ShouldTrace() {
		ctx = httpx.WithTrace(ctx, httpx.NewTrace())
	}
	tr := httpx.TraceFrom(ctx)
	model := requestedModel(req.Body)
	tr.Add("inbound %s model=%s stream=%v", req.Inbound, model, req.Stream)
	if model == "" {
		return failOut(req, 400, httpx.KindRequest, "", "", "model is required", `set "model" in the request body`), nil
	}
	if modelDisabled(DisabledModels(hub), model) {
		httpx.DumpTrace(ctx, true, "model turned off")
		return failOut(req, 404, httpx.KindModel, "", model, model+" is turned off in Mainspring", "turn it on under Sources → Models in the panel, or pick another model from GET /v1/models"), nil
	}
	chain := resolveProviderChain(hub, model)
	if len(chain) == 0 {
		httpx.DumpTrace(ctx, true, "unknown model")
		return failOut(req, 404, httpx.KindModel, "", model, "unknown model: "+model, "GET /v1/models for ids this instance can serve"), nil
	}
	for _, h := range chain {
		tr.Add("hop %s/%s", h.Provider, h.Model)
	}
	if req.Key.DailyCap != nil {
		used, err := hub.Usage.TodayTokensForKey(req.Key.ID, Zone(hub))
		if err != nil {
			return Result{}, err
		}
		if used >= *req.Key.DailyCap {
			return failOut(req, 429, httpx.KindCap, "", model, "daily token cap reached", "wait until midnight ("+Zone(hub).String()+"), or raise the key's cap in the panel"), nil
		}
	}

	router := hub.router()
	bodyMap, _ := req.Body.(map[string]any)
	sessionKey := route.SessionKey(req.InboundHeaders, bodyMap, req.Key.ID)
	client := route.ClientName(req.InboundHeaders)
	transport := req.Transport
	if transport == "" {
		transport = "http"
	}

	var last provider.ExecuteResult
	var lastHop hop
	var restingUntil int64
	restingProvider := domain.ProviderID("")
	attempted := false
	firstByte := false
	routeName := string(req.Inbound)

	for hopIndex, hop := range chain {
		if err := hub.Keys.Allows(req.Key, hop.Provider, hop.Model); err != nil {
			last = provider.ExecuteResult{OK: false, Status: 403, Message: err.Error()}
			lastHop = hop
			continue
		}
		executor := hub.Executors[hop.Provider]
		if executor == nil {
			last = provider.ExecuteResult{OK: false, Status: 503, Message: "provider " + string(hop.Provider) + " is not available"}
			lastHop = hop
			continue
		}
		inboundBody := req.Body
		execFormat := provider.FormatFor(executor, hop.Model)
		if req.Inbound == domain.InboundOpenAIResponses && execFormat != domain.FormatCodex {
			inboundBody = translate.FlattenCodexMultiAgent(req.Body)
		}
		translated := translate.TranslateRequest(req.Inbound, execFormat, hop.Model, req.Stream, inboundBody)
		all, err := hopCredentials(hub, hop.Provider, hop.Model)
		if err != nil {
			return Result{}, err
		}
		if len(all) == 0 {
			last = provider.ExecuteResult{OK: false, Status: 503, Message: "no " + string(hop.Provider) + " credential is connected"}
			lastHop = hop
			continue
		}
		candidates, backAt := router.Order(hop.Provider, all, sessionKey)
		if len(candidates) == 0 {
			tr.Add("%s: every account is resting", hop.Provider)
			if restingUntil == 0 || (backAt > 0 && backAt < restingUntil) {
				restingUntil, restingProvider = backAt, hop.Provider
			}
			if restingProvider == "" {
				restingProvider = hop.Provider
			}
			noteAllOut(hub, hop.Provider, backAt)
			last = provider.ExecuteResult{OK: false, Status: 429, Retryable: true, Message: "every " + providerName(hop.Provider) + " account is resting"}
			lastHop = hop
			continue
		}
		attempted = true
		for i, cand := range candidates {
			cred := cand.Credential
			if nearExpiry(cred) {
				if refreshed, err := refreshCredential(ctx, hub, executor, cred); err == nil {
					cred = refreshed
				} else {
					logRefresh(hop.Provider, cred.Label, routeName, err)
					if signInRejected(hub, cred, err) {
						last, lastHop = provider.ExecuteResult{OK: false, Status: 401, Retryable: true, Message: "sign-in rejected"}, hop
						continue
					}
				}
			}
			opts := provider.ExecuteOptions{
				Stream: req.Stream, CountTokens: req.CountTokens, Compact: req.Compact,
				InboundHeaders: req.InboundHeaders, CallerKey: req.Key.ID,
				CacheTTL: cacheTTL(hop.Provider, sessionKey, translated.Body),
			}
			result, err := executor.Execute(ctx, cred, translated.Body, opts)
			if err != nil {
				result = provider.FailFromErr(err)
			}
			if !result.OK && result.Status == 401 {
				if refreshed, err := refreshCredential(ctx, hub, executor, cred); err == nil {
					cred = refreshed
					result, err = executor.Execute(ctx, cred, translated.Body, opts)
					if err != nil {
						result = provider.FailFromErr(err)
					}
				} else {
					logRefresh(hop.Provider, cred.Label, routeName, err)
					signInRejected(hub, cred, err)
				}
			}
			observe(hub, cred, result)
			if !result.OK {
				last, lastHop = result, hop
				moreCreds := result.Retryable && i < len(candidates)-1 && !firstByte
				moreHops := result.Retryable && hopIndex < len(chain)-1 && !firstByte
				if moreCreds {
					continue
				}
				if moreHops {
					break
				}
				res := withIncident(hub, hop.Provider, result.Status, failFromExec(req, hop.Provider, hop.Model, result))
				recordFail(hub, req, hop.Provider, hop.Model, cred.ID, started, res)
				httpx.DumpTrace(ctx, true, fmt.Sprintf("%s/%s status=%d", hop.Provider, hop.Model, result.Status))
				return res, nil
			}
			router.Succeeded(cred.ID)
			movedFrom := router.Bind(hop.Provider, sessionKey, cred.ID)
			if movedFrom != "" {
				noteMoved(hub, hop.Provider, movedFrom, cred)
			}
			touch := route.Touch{
				Provider: hop.Provider, SessionKey: sessionKey, CredID: cred.ID, LocalKeyID: req.Key.ID,
				Client: client, Model: hop.Model, Transport: transport, MovedFrom: movedFrom,
			}
			if movedFrom != "" {
				touch.MovedReason = movedReason(hub, movedFrom)
			}
			passthrough := accountHeaders(hub, passthroughHeaders(result.Header), hop.Provider, cred, movedFrom, touch.MovedReason)
			if req.Stream && result.Stream != nil {
				out := make(chan string)
				go func() {
					defer close(out)
					state := translate.EmptyStreamState()
					sentDone := false
					for chunk := range result.Stream {
						firstByte = true
						lines := translate.TranslateStream(req.Inbound, execFormat, hop.Model, req.Body, chunk, &state)
						for _, line := range lines {
							if line == "data: [DONE]" {
								sentDone = true
							}
							if !strings.HasSuffix(line, "\n") {
								line += "\n\n"
							}
							out <- line
						}
					}
					if done := translate.DoneLine(req.Inbound); done != "" && !sentDone {
						out <- done + "\n\n"
					}
					status := "ok"
					if state.Error != "" || !state.Finished {
						status = "error"
						logIncompleteStream(string(hop.Provider), hop.Model, routeName, state.Error)
						httpx.DumpTrace(ctx, true, fmt.Sprintf("%s/%s unfinished stream", hop.Provider, hop.Model))
					} else {
						httpx.LogOK(string(hop.Provider), hop.Model, routeName)
						httpx.DumpTrace(ctx, false, fmt.Sprintf("%s/%s ok", hop.Provider, hop.Model))
					}
					u := usageCounts{state.PromptTokens, state.CompletionTokens, state.CacheRead, state.CacheWrite}
					touch.InputTokens, touch.CacheRead = InputTokens(hop.Provider, u.prompt, u.cacheRead, u.cacheWrite), u.cacheRead
					router.Touch(touch)
					if status == "error" {
						msg := "the answer stopped before it finished"
						if state.Error != "" {
							msg += ": " + httpx.CleanMessage(state.Error)
						}
						if in, ok := hub.Status.For(hop.Provider); ok {
							msg += " (" + in.Company + " reports an incident: " + in.Name + ", " + in.URL + ")"
						}
						recordEvent(hub, req, hop.Provider, hop.Model, cred.ID, started, status, u, state.RoutedModel, 0, msg)
					} else {
						record(hub, req, hop.Provider, hop.Model, cred.ID, started, status, u, state.RoutedModel)
					}
				}()
				return Result{OK: true, Status: 200, Stream: out, Header: passthrough}, nil
			}
			body := translate.TranslateResponse(req.Inbound, execFormat, hop.Model, req.Body, result.Body)
			pt, ct, cr, cw := usageFrom(body, result.Body)
			httpx.LogOK(string(hop.Provider), hop.Model, routeName)
			httpx.DumpTrace(ctx, false, fmt.Sprintf("%s/%s ok", hop.Provider, hop.Model))
			touch.InputTokens, touch.CacheRead = InputTokens(hop.Provider, pt, cr, cw), cr
			if !req.CountTokens {
				router.Touch(touch)
			}
			record(hub, req, hop.Provider, hop.Model, cred.ID, started, "ok", usageCounts{pt, ct, cr, cw}, routedModel(body, result.Body))
			return Result{OK: true, Status: 200, Body: body, Header: passthrough}, nil
		}
	}
	if lastHop.Provider == "" {
		lastHop = chain[0]
	}
	if restingProvider != "" && (last.Status == 429 || !attempted) {
		res := allResting(req, restingProvider, lastHop.Model, restingUntil)
		recordFail(hub, req, lastHop.Provider, lastHop.Model, "", started, res)
		return res, nil
	}
	if last.Status == 0 {
		if attempted {
			last.Status = 502
		} else {
			last.Status = 503
		}
		if last.Message == "" {
			last.Message = "all credentials failed"
		}
	}
	res := withIncident(hub, lastHop.Provider, last.Status, failFromExec(req, lastHop.Provider, lastHop.Model, last))
	recordFail(hub, req, lastHop.Provider, lastHop.Model, "", started, res)
	httpx.DumpTrace(ctx, true, fmt.Sprintf("%s/%s status=%d", lastHop.Provider, lastHop.Model, last.Status))
	return res, nil
}

func ListModels(hub *Hub) []domain.ModelInfo {
	creds, _ := hub.Vault.List()
	seenProv := map[domain.ProviderID]bool{}
	var native []domain.ProviderID
	cursorOn := false
	var openaiOn []domain.Credential
	var opencodeOn []domain.Credential
	for _, c := range creds {
		switch c.Provider {
		case domain.ProviderOpenAI:
			openaiOn = append(openaiOn, c)
			continue
		case domain.ProviderOpenCode:
			opencodeOn = append(opencodeOn, c)
			continue
		}
		if seenProv[c.Provider] {
			continue
		}
		seenProv[c.Provider] = true
		if c.Provider == domain.ProviderCursor {
			cursorOn = true
			continue
		}
		native = append(native, c.Provider)
	}
	prefix := false
	listFast := true
	if hub.Settings != nil {
		v, _ := hub.Settings.Get(SettingCursorPrefix)
		prefix = v == "1"
		listFast = settingOnByDefault(hub, SettingCursorListFast)
	}
	seenID := map[string]bool{}
	var models []domain.ModelInfo
	var addMu sync.Mutex
	add := func(list []domain.ModelInfo) {
		addMu.Lock()
		defer addMu.Unlock()
		for _, m := range list {
			if seenID[m.ID] {
				continue
			}
			seenID[m.ID] = true
			models = append(models, m)
		}
	}
	for _, providerID := range native {
		ex := hub.Executors[providerID]
		if ex == nil {
			continue
		}
		if offered := accountModels(hub, ex, providerID); len(offered) > 0 {
			add(offered)
			continue
		}
		add(ex.Models())
	}
	if cursorOn {
		if ex := hub.Executors[domain.ProviderCursor]; ex != nil {
			add(cursorModelsForList(ex, prefix, listFast))
		}
	}
	if live, ok := hub.Executors[domain.ProviderOpenAI].(liveModels); ok && len(openaiOn) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		for _, c := range openaiOn {
			wg.Add(1)
			go func(c domain.Credential) {
				defer wg.Done()
				add(live.ModelsFor(ctx, c))
			}(c)
		}
		wg.Wait()
	}
	if len(opencodeOn) > 0 {
		if live, ok := hub.Executors[domain.ProviderOpenCode].(liveModels); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			add(live.ModelsFor(ctx, opencodeOn[0]))
		} else if ex := hub.Executors[domain.ProviderOpenCode]; ex != nil {
			add(ex.Models())
		}
	}
	if models == nil {
		models = []domain.ModelInfo{}
	}
	return models
}

func cursorModelsForList(ex provider.Executor, prefix, listFast bool) []domain.ModelInfo {
	models := ex.Models()
	if !listFast {
		filtered := make([]domain.ModelInfo, 0, len(models))
		for _, m := range models {
			if strings.HasSuffix(cursor.StripPublicPrefix(m.ID), "-fast") {
				continue
			}
			filtered = append(filtered, m)
		}
		models = filtered
	}
	if !prefix {
		return models
	}
	out := make([]domain.ModelInfo, len(models))
	for i, m := range models {
		if !strings.HasPrefix(m.ID, "cursor/") {
			m.ID = "cursor/" + m.ID
		}
		out[i] = m
	}
	return out
}

type hop struct {
	Provider domain.ProviderID
	Model    string
}

func resolveProviderChain(hub *Hub, rawModel string) []hop {
	if strings.HasPrefix(rawModel, "opencode/") {
		if opencodeConnected(hub) {
			return []hop{{domain.ProviderOpenCode, rawModel}}
		}
		return nil
	}
	if hops := openaiSourceHops(hub, rawModel); len(hops) > 0 {
		return hops
	}
	forced := strings.HasPrefix(rawModel, "cursor/")
	model := cursor.StripPublicPrefix(rawModel)
	known := cursor.KnownIDs()
	restored := cursor.RestoreWirePrefix(model, known)
	cursorOK := cursorConnected(hub)
	if forced || strings.HasSuffix(model, "-fast") || strings.HasSuffix(restored, "-fast") || strings.HasPrefix(restored, "composer-") || strings.HasPrefix(restored, "cursor-") || strings.HasPrefix(restored, "gemini-") || strings.HasPrefix(restored, "kimi-") || strings.HasPrefix(restored, "glm-") || restored == "cursor-auto" {
		var hops []hop
		if cursorOK {
			hops = append(hops, hop{domain.ProviderCursor, restored})
		}
		if len(hops) > 0 && grokFailoverOn(hub) && strings.Contains(restored, "grok") {
			if p, native, ok := cursor.MapWireToNative(restored); ok {
				list, _ := hub.Vault.List(p)
				if len(list) > 0 {
					hops = append(hops, hop{p, native})
				}
			}
		}
		return hops
	}
	if strings.HasPrefix(model, "claude-") {
		hops := []hop{{domain.ProviderClaude, model}}
		if cursorOK {
			if mapped := cursor.MapNativeToWire(model, known); mapped != "" {
				hops = append(hops, hop{domain.ProviderCursor, mapped})
			}
		}
		return hops
	}
	if strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") {
		hops := []hop{{domain.ProviderCodex, model}}
		if cursorOK {
			if mapped := cursor.MapNativeToWire(model, known); mapped != "" {
				hops = append(hops, hop{domain.ProviderCursor, mapped})
			}
		}
		return hops
	}
	if strings.HasPrefix(model, "grok-") {
		hops := []hop{{domain.ProviderGrok, model}}
		if grokFailoverOn(hub) && cursorOK {
			if mapped := cursor.MapNativeToWire(model, known); mapped != "" {
				hops = append(hops, hop{domain.ProviderCursor, mapped})
			}
		}
		return hops
	}
	if domain.IsProviderID(model) {
		return []hop{{domain.ProviderID(model), model}}
	}
	creds, _ := hub.Vault.List()
	for _, c := range creds {
		if c.Provider == domain.ProviderOpenAI || c.Provider == domain.ProviderOpenCode {
			continue
		}
		ex := hub.Executors[c.Provider]
		if ex == nil {
			continue
		}
		for _, m := range ex.Models() {
			if m.ID == model {
				return []hop{{c.Provider, model}}
			}
		}
	}
	return nil
}

func opencodeConnected(hub *Hub) bool {
	list, _ := hub.Vault.List(domain.ProviderOpenCode)
	return len(list) > 0
}

func openaiSourceHops(hub *Hub, model string) []hop {
	list, _ := hub.Vault.List(domain.ProviderOpenAI)
	for _, c := range list {
		slug := openai.SourceSlug(c)
		if slug != "" && strings.HasPrefix(model, slug+"/") {
			return []hop{{domain.ProviderOpenAI, model}}
		}
	}
	return nil
}

func cursorConnected(hub *Hub) bool {
	list, _ := hub.Vault.List(domain.ProviderCursor)
	return len(list) > 0
}

func grokFailoverOn(hub *Hub) bool {
	if hub.Settings == nil {
		return false
	}
	v, _ := hub.Settings.Get(SettingCursorGrokFailover)
	return v == "1"
}

func settingOnByDefault(hub *Hub, key string) bool {
	if hub == nil || hub.Settings == nil {
		return true
	}
	v, ok := hub.Settings.Get(key)
	if !ok {
		return true
	}
	return v != "0"
}

func syncTraceMode(hub *Hub) {
	mode := httpx.TraceOff
	if hub != nil && hub.Settings != nil {
		if v, ok := hub.Settings.Get(SettingLogTrace); ok {
			mode = v
		}
	}
	httpx.SetTraceMode(mode)
}

func requestedModel(body any) string {
	m, _ := body.(map[string]any)
	s, _ := m["model"].(string)
	return s
}

func nearExpiry(c domain.Credential) bool {
	return expiresWithin(c, 5*time.Minute)
}

func expiresWithin(c domain.Credential, d time.Duration) bool {
	return c.ExpiresAt != nil && *c.ExpiresAt-time.Now().UnixMilli() < d.Milliseconds()
}

// KeepaliveAhead is how long before expiry the keepalive refreshes a token.
// It runs every UsageMaxAge, so anything expiring before the next run plus a
// margin is refreshed now, in the background and one account at a time,
// instead of inside a burst of requests.
const KeepaliveAhead = UsageMaxAge + 30*time.Minute

const keepaliveSkewMS = 5 * 60 * 1000

func KeepaliveCredentials(ctx context.Context, hub *Hub) (int, error) {
	if hub == nil || hub.Vault == nil {
		return 0, nil
	}
	creds, err := hub.Vault.List()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, cred := range creds {
		if !expiresWithin(cred, KeepaliveAhead) || hub.router().Get(cred.ID).NeedsSignIn {
			continue
		}
		ex := hub.Executors[cred.Provider]
		if ex == nil {
			continue
		}
		refreshed, err := refreshCredential(ctx, hub, ex, cred)
		if err != nil {
			logRefresh(cred.Provider, cred.Label, "keepalive", err)
			signInRejected(hub, cred, err)
			continue
		}
		if !credentialAdvanced(cred, refreshed) {
			continue
		}
		n++
	}
	return n, nil
}

func credentialAdvanced(before, after domain.Credential) bool {
	if after.Tokens.AccessToken != "" && after.Tokens.AccessToken != before.Tokens.AccessToken {
		return true
	}
	if after.ExpiresAt == nil {
		return false
	}
	if before.ExpiresAt == nil {
		return true
	}
	return *after.ExpiresAt > *before.ExpiresAt+keepaliveSkewMS/2
}

func usageFrom(translated, upstream any) (pt, ct, cr, cw int64) {
	t := asMap(translated)
	u := asMap(upstream)
	usage := asMap(t["usage"])
	if len(usage) == 0 {
		usage = asMap(u["usage"])
	}
	details := asMap(usage["prompt_tokens_details"])
	if len(details) == 0 {
		details = asMap(usage["input_tokens_details"])
	}
	pt = int64From(usage["prompt_tokens"], usage["input_tokens"])
	ct = int64From(usage["completion_tokens"], usage["output_tokens"])
	cr = int64From(details["cached_tokens"], usage["cache_read_input_tokens"])
	cw = int64From(details["cache_write_tokens"], usage["cache_creation_input_tokens"])
	return
}

// recordFail records a failed request with what its client was told.
func recordFail(hub *Hub, req Request, providerID domain.ProviderID, model, credID string, started int64, res Result) {
	recordEvent(hub, req, providerID, model, credID, started, "error", usageCounts{}, "", res.Status, httpx.ExtractErrorMessage(res.Body))
}

func record(hub *Hub, req Request, providerID domain.ProviderID, model, credID string, started int64, status string, u usageCounts, routed string) {
	recordEvent(hub, req, providerID, model, credID, started, status, u, routed, 0, "")
}

func recordEvent(hub *Hub, req Request, providerID domain.ProviderID, model, credID string, started int64, status string, u usageCounts, routed string, errStatus int, errMessage string) {
	pt, ct, cr, cw := u.prompt, u.completion, u.cacheRead, u.cacheWrite
	priceProvider := map[domain.ProviderID]string{
		domain.ProviderClaude:   "anthropic",
		domain.ProviderCodex:    "openai",
		domain.ProviderGrok:     "xai",
		domain.ProviderCursor:   "cursor",
		domain.ProviderOpenAI:   "openai",
		domain.ProviderOpenCode: "opencode",
	}[providerID]
	meterModel := model
	if routed != "" {
		meterModel = routed
	}
	usd := hub.Prices.Estimate(priceProvider, meterModel, pt, ct, cr, cw)
	if usd == 0 && (providerID == domain.ProviderOpenAI || providerID == domain.ProviderOpenCode) {
		if i := strings.Index(meterModel, "/"); i >= 0 {
			usd = hub.Prices.Estimate(priceProvider, meterModel[i+1:], pt, ct, cr, cw)
		}
	}
	_ = hub.Usage.Record(domain.UsageEvent{
		TS: time.Now().UnixMilli(), KeyID: req.Key.ID, CredentialID: credID, Provider: providerID, Model: meterModel, Inbound: req.Inbound,
		PromptTokens: pt, CompletionTokens: ct, CacheReadTokens: cr, CacheWriteTokens: cw,
		LatencyMs: time.Now().UnixMilli() - started, Status: status, USD: usd,
		ErrorStatus: errStatus, ErrorMessage: errMessage,
	})
}

func routedModel(translated, upstream any) string {
	for _, src := range []any{translated, upstream} {
		usage := asMap(asMap(src)["usage"])
		if s, _ := usage["routed_model"].(string); s != "" {
			return s
		}
	}
	return ""
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func int64From(vals ...any) int64 {
	for _, v := range vals {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case int:
			return int64(n)
		}
	}
	return 0
}

// accountModels is the union of what the provider's accounts offer, in the
// providers' own order, or nil when none could be asked.
func accountModels(hub *Hub, ex provider.Executor, providerID domain.ProviderID) []domain.ModelInfo {
	live, ok := ex.(liveModels)
	if !ok {
		return nil
	}
	creds, _ := hub.Vault.List(providerID)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	lists := make([][]domain.ModelInfo, len(creds))
	var wg sync.WaitGroup
	for i, c := range creds {
		if hub.Router != nil && hub.Router.Get(c.ID).NeedsSignIn {
			continue
		}
		wg.Add(1)
		go func(i int, c domain.Credential) {
			defer wg.Done()
			lists[i] = live.ModelsFor(ctx, c)
		}(i, c)
	}
	wg.Wait()
	seen := map[string]bool{}
	var out []domain.ModelInfo
	for _, list := range lists {
		for _, m := range list {
			if !seen[m.ID] {
				seen[m.ID] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// cacheTTL picks how long Claude keeps a request's prompt cache. A session
// stays on its account for an hour, so its cache should last as long: a pause
// over five minutes would otherwise write the whole prompt again. One-off
// requests (no session, or a single message without tools, like a title or a
// summary) keep the cheaper five-minute default.
func cacheTTL(p domain.ProviderID, sessionKey string, body any) string {
	if p != domain.ProviderClaude || sessionKey == "" {
		return ""
	}
	b := asMap(body)
	if len(translate.AsArr(b["tools"])) == 0 && len(translate.AsArr(b["messages"])) <= 1 {
		return ""
	}
	return "1h"
}
