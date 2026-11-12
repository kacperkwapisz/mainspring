package app

import (
	"math/rand"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

// SeedDemo fills an empty instance with a believable pool so the panel can be
// tried, tested and screenshotted without real accounts. `mainspring demo`
// uses it with an in-memory database; none of these credentials work upstream.
func SeedDemo(b *Booted) error {
	hub := b.Hub
	now := time.Now()
	ms := func(d time.Duration) *int64 { v := now.Add(d).UnixMilli(); return &v }
	used := func(p float64) *float64 { return &p }
	add := func(p domain.ProviderID, label, plan string, extra map[string]any) domain.Credential {
		if extra == nil {
			extra = map[string]any{}
		}
		if plan != "" {
			extra["plan"] = plan
		}
		exp := now.Add(8 * time.Hour).UnixMilli()
		c, _ := hub.Vault.Save(store.SaveCredential{Provider: p, Label: label, ExpiresAt: &exp,
			Tokens: domain.CredentialTokens{AccessToken: "demo", RefreshToken: "demo", Email: label, Extra: extra}})
		return c
	}
	claudeWin := func(fiveLeft, weekLeft float64, fiveIn, weekIn time.Duration) []route.Window {
		return []route.Window{
			{ID: "claude-5h", Label: "5 hours", UsedPercent: used(100 - fiveLeft), ResetsAt: ms(fiveIn), PeriodMS: 5 * 3600_000},
			{ID: "claude-7d", Label: "7 days", UsedPercent: used(100 - weekLeft), ResetsAt: ms(weekIn), PeriodMS: 7 * 24 * 3600_000},
		}
	}
	r := hub.Router
	h := time.Hour
	kacper := add(domain.ProviderClaude, "alex@example.com", "Max 20x", nil)
	dubia := add(domain.ProviderClaude, "sam@example.com", "Max 20x", nil)
	work := add(domain.ProviderClaude, "work@example.com", "Max 20x", nil)
	studio := add(domain.ProviderClaude, "studio@example.com", "Max 5x", nil)
	kk := add(domain.ProviderClaude, "personal@example.com", "Max 20x", nil)
	bb := add(domain.ProviderClaude, "spare@example.com", "Max 20x", nil)
	old := add(domain.ProviderClaude, "old@example.com", "Max 20x", nil)
	r.ObserveWindows(kacper.ID, "headers", claudeWin(38, 19, h+45*time.Minute, 19*h))
	r.ObserveWindows(dubia.ID, "headers", claudeWin(67, 33, 3*h+10*time.Minute, 42*h))
	r.ObserveWindows(work.ID, "headers", claudeWin(88, 56, 4*h+20*time.Minute, 78*h))
	r.ObserveWindows(kk.ID, "headers", claudeWin(0, 42, 50*time.Minute, 52*h))
	r.RateLimited(kk.ID, route.Limit{Until: now.Add(50 * time.Minute).UnixMilli(), Label: "5 hours", Account: true})
	r.SetBenched(bb.ID, true)
	r.MarkSignIn(old.ID, "claude refresh failed: 400")
	// studio@ has quota data from two days ago, so it shows as stale.
	r.ObserveWindows(studio.ID, "usage", claudeWin(100, 82, 0, 141*h))
	r.Backdate(studio.ID, now.Add(-50*h).UnixMilli())

	ck := add(domain.ProviderCodex, "alex@example.com", "Pro", nil)
	cw := add(domain.ProviderCodex, "work@example.com", "Plus", nil)
	cd := add(domain.ProviderCodex, "sam@example.com", "Plus", nil)
	codexWin := func(fiveLeft, weekLeft float64, fiveIn, weekIn time.Duration) []route.Window {
		return []route.Window{
			{ID: "codex-primary", Label: "5 hours", UsedPercent: used(100 - fiveLeft), ResetsAt: ms(fiveIn), PeriodMS: 5 * 3600_000},
			{ID: "codex-secondary", Label: "7 days", UsedPercent: used(100 - weekLeft), ResetsAt: ms(weekIn), PeriodMS: 7 * 24 * 3600_000},
		}
	}
	r.ObserveWindows(ck.ID, "headers", codexWin(59, 28, h+35*time.Minute, 37*h))
	r.ObserveWindows(cw.ID, "headers", codexWin(95, 77, 4*h+50*time.Minute, 112*h))
	r.Backdate(cw.ID, now.Add(-5*h).UnixMilli())
	r.ObserveWindows(cd.ID, "headers", codexWin(100, 0, 0, 68*h))
	r.RateLimited(cd.ID, route.Limit{Until: now.Add(68 * time.Hour).UnixMilli(), Label: "7 days", Account: true})

	cursor := add(domain.ProviderCursor, "alex@example.com", "Pro", nil)
	grok := add(domain.ProviderGrok, "alex@example.com", "SuperGrok", nil)
	oc := add(domain.ProviderOpenCode, "Service key", "", nil)
	add(domain.ProviderOpenAI, "openrouter", "", map[string]any{"base_url": "http://127.0.0.1:9/v1", "slug": "openrouter", "kind": "openai"})

	// Keys.
	keyIDs := map[string]string{}
	for _, k := range []struct {
		name string
		cap  int64
	}{{"macbook", 0}, {"mini", 0}, {"devbox", 20_000_000}, {"t3code", 0}, {"cursor", 5_000_000}} {
		var cp *int64
		if k.cap > 0 {
			c := k.cap
			cp = &c
		}
		created, err := hub.Keys.Create(k.name, nil, nil, cp)
		if err == nil {
			keyIDs[k.name] = created.Key.ID
		}
	}

	// Live sessions.
	for _, s := range []struct {
		p              domain.ProviderID
		key, cred, mdl string
		client, tr     string
		started        time.Duration
		last           time.Duration
		reqs           int64
		share          float64
		moved          string
	}{
		{domain.ProviderClaude, "macbook", kacper.ID, "claude-opus-5", "Claude Code", "http", 78 * time.Minute, 12 * time.Second, 214, .94, ""},
		{domain.ProviderClaude, "macbook", kacper.ID, "claude-sonnet-5", "Claude Code", "http", 40 * time.Minute, time.Minute, 88, .92, ""},
		{domain.ProviderClaude, "devbox", kacper.ID, "claude-opus-5", "Claude Code", "http", 15 * time.Minute, 40 * time.Second, 31, .89, ""},
		{domain.ProviderClaude, "mini", dubia.ID, "claude-opus-5", "Claude Code", "http", 170 * time.Minute, 2 * time.Minute, 160, .71, kk.ID},
		{domain.ProviderClaude, "t3code", dubia.ID, "claude-sonnet-5", "Claude Code", "http", 130 * time.Minute, 5 * time.Minute, 74, .68, kk.ID},
		{domain.ProviderClaude, "devbox", work.ID, "claude-haiku-5", "Claude Code", "http", 5 * time.Minute, 3 * time.Minute, 12, .81, ""},
		{domain.ProviderCodex, "macbook", ck.ID, "gpt-5.5-codex", "Codex", "websocket", 240 * time.Minute, 20 * time.Second, 130, .90, cd.ID},
		{domain.ProviderCodex, "t3code", ck.ID, "gpt-5.5", "Codex", "http", 25 * time.Minute, 4 * time.Minute, 41, .87, ""},
	} {
		in := s.reqs * 90_000
		sess := route.Session{
			Key: string(s.p) + "|demo-" + randID(), Provider: s.p, CredID: s.cred, LocalKeyID: keyIDs[s.key],
			Client: s.client, Model: s.mdl, Transport: s.tr, StartedAt: now.Add(-s.started).UnixMilli(),
			LastAt: now.Add(-s.last).UnixMilli(), Requests: s.reqs, InputTokens: in, CacheRead: int64(float64(in) * s.share),
		}
		sess.ShortID = sess.Key[len(sess.Key)-6:]
		if s.moved != "" {
			sess.MovedFrom, sess.MovedAt = s.moved, now.Add(-6*time.Minute).UnixMilli()
			sess.MovedReason = "5-hour limit hit"
			if s.p == domain.ProviderCodex {
				sess.MovedAt, sess.MovedReason = now.Add(-3*time.Hour).UnixMilli(), "7-day limit hit"
			}
		}
		r.SeedSession(sess)
	}

	// Usage over the last 14 days.
	rng := rand.New(rand.NewSource(7))
	type mix struct {
		p     domain.ProviderID
		cred  string
		model string
		w     int
	}
	mixes := []mix{
		{domain.ProviderClaude, kacper.ID, "claude-opus-5", 30}, {domain.ProviderClaude, dubia.ID, "claude-opus-5", 18},
		{domain.ProviderClaude, work.ID, "claude-sonnet-5", 14}, {domain.ProviderClaude, kk.ID, "claude-haiku-5", 8},
		{domain.ProviderCodex, ck.ID, "gpt-5.5-codex", 16}, {domain.ProviderCodex, cd.ID, "gpt-5.5", 4},
		{domain.ProviderCursor, cursor.ID, "composer-2.5", 2}, {domain.ProviderGrok, grok.ID, "grok-4.6", 1},
		{domain.ProviderOpenCode, oc.ID, "opencode/kimi-k3", 1},
	}
	total := 0
	for _, m := range mixes {
		total += m.w
	}
	keys := []string{keyIDs["macbook"], keyIDs["mini"], keyIDs["devbox"], keyIDs["t3code"]}
	for day := 9; day >= 0; day-- {
		n := 260 + rng.Intn(220)
		if day == 0 {
			n = int(float64(now.Hour()*60+now.Minute()) / (24 * 60) * 1700)
		}
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -day)
		for i := 0; i < n; i++ {
			pick := rng.Intn(total)
			var m mix
			for _, x := range mixes {
				if pick < x.w {
					m = x
					break
				}
				pick -= x.w
			}
			span := 24 * time.Hour
			if day == 0 {
				span = now.Sub(dayStart)
			}
			ts := dayStart.Add(time.Duration(rng.Int63n(int64(span)))).UnixMilli()
			prompt := int64(2000 + rng.Intn(8000))
			cache := int64(30000 + rng.Intn(90000))
			out := int64(500 + rng.Intn(4000))
			pp := map[domain.ProviderID]string{domain.ProviderClaude: "anthropic", domain.ProviderCodex: "openai", domain.ProviderGrok: "xai", domain.ProviderCursor: "cursor", domain.ProviderOpenCode: "opencode"}[m.p]
			if m.p != domain.ProviderClaude {
				prompt += cache
			}
			usd := hub.Prices.Estimate(pp, m.model, prompt, out, cache, 800)
			_ = hub.Usage.Record(domain.UsageEvent{TS: ts, KeyID: keys[rng.Intn(len(keys))], CredentialID: m.cred, Provider: m.p, Model: m.model,
				Inbound: domain.InboundClaudeMessages, PromptTokens: prompt, CompletionTokens: out, CacheReadTokens: cache, CacheWriteTokens: 800,
				LatencyMs: 2000, Status: "ok", USD: usd})
		}
		// Web tools: a few dozen searches and page questions a day, on Claude and Codex.
		for i := 0; i < 20+rng.Intn(30); i++ {
			ts := dayStart.Add(time.Duration(rng.Int63n(int64(24 * time.Hour)))).UnixMilli()
			if ts > now.UnixMilli() {
				continue
			}
			inbound, p, model, cred := domain.InboundFormat("mcp.web_search"), domain.ProviderClaude, "claude-haiku-5-5", kacper.ID
			if rng.Intn(4) == 0 {
				inbound = "mcp.web_fetch"
			}
			if rng.Intn(3) == 0 {
				p, model, cred = domain.ProviderCodex, "gpt-5.6-luna", ck.ID
			}
			_ = hub.Usage.Record(domain.UsageEvent{TS: ts, KeyID: keys[rng.Intn(len(keys))], CredentialID: cred, Provider: p, Model: model,
				Inbound: inbound, PromptTokens: int64(3000 + rng.Intn(6000)), CompletionTokens: int64(200 + rng.Intn(600)), LatencyMs: 4000, Status: "ok"})
		}
	}

	// A few failed requests, with what their apps were told, and their log lines.
	for _, f := range []struct {
		ago     time.Duration
		p       domain.ProviderID
		cred    string
		model   string
		key     string
		status  int
		message string
		inbound domain.InboundFormat
		kind    string
	}{
		{26*time.Hour + 17*time.Minute, domain.ProviderCursor, "", "composer-2.5", "devbox", 502, "Cursor answered 502: Bad Gateway", domain.InboundOpenAIChat, httpx.KindUpstream},
		{20*time.Hour + 32*time.Minute, domain.ProviderCodex, cd.ID, "gpt-5.5", "mini", 401, "Codex rejected this account's sign-in. Sign in again from the Pool page", domain.InboundOpenAIResponses, httpx.KindAuth},
		{2*time.Hour + 4*time.Minute, domain.ProviderClaude, dubia.ID, "claude-sonnet-5", "t3code", 400, "prompt is too long: 212,410 tokens > 200,000 maximum", domain.InboundClaudeMessages, httpx.KindRequest},
		{88 * time.Minute, domain.ProviderCodex, ck.ID, "gpt-5.5-codex", "devbox", 0, "the answer stopped before it finished: upstream connection reset", domain.InboundOpenAIResponses, httpx.KindUpstream},
		{6 * time.Minute, domain.ProviderClaude, "", "claude-opus-5", "macbook", 429, "every Claude account is resting: the first one is back at " + now.Add(50*time.Minute).Format("15:04") + " (in 50 min)", domain.InboundOpenAIChat, httpx.KindCap},
	} {
		ts := now.Add(-f.ago)
		_ = hub.Usage.Record(domain.UsageEvent{TS: ts.UnixMilli(), KeyID: keyIDs[f.key], CredentialID: f.cred, Provider: f.p, Model: f.model,
			Inbound: f.inbound, LatencyMs: 48000, Status: "error", ErrorStatus: f.status, ErrorMessage: f.message})
		if f.ago < 3*time.Hour {
			httpx.Fail{Kind: f.kind, Status: f.status, Provider: string(f.p), Model: f.model, Route: string(f.inbound), Message: f.message}.Log()
		}
	}

	// What happened today.
	hub.Activity.AddAt(now.Add(-198*time.Minute).UnixMilli(), store.ActivityLimit, "codex", cd.ID, map[string]any{"account": cd.Label, "window": "7 days", "until": now.Add(68 * time.Hour).UnixMilli(), "moved": 1.0, "to": ck.Label})
	hub.Activity.AddAt(now.Add(-126*time.Minute).UnixMilli(), store.ActivitySignIn, "claude", old.ID, map[string]any{"account": old.Label})
	hub.Activity.AddAt(now.Add(-6*time.Minute).UnixMilli(), store.ActivityLimit, "claude", kk.ID, map[string]any{"account": kk.Label, "window": "5 hours", "until": now.Add(50 * time.Minute).UnixMilli(), "moved": 2.0, "to": dubia.Label})
	return nil
}

func randID() string {
	const alphabet = "0123456789abcdef"
	b := make([]byte, 12)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}
