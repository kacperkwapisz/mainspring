package route

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

func hdr(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestClaudeWindowsAreDynamic(t *testing.T) {
	now := time.Now().Unix()
	h := hdr(
		"anthropic-ratelimit-unified-status", "allowed",
		"anthropic-ratelimit-unified-5h-utilization", "0.62",
		"anthropic-ratelimit-unified-5h-reset", itoa(now+3600),
		"anthropic-ratelimit-unified-7d-utilization", "0.81",
		"anthropic-ratelimit-unified-7d-reset", itoa(now+86400),
		"anthropic-ratelimit-unified-7d_oi-utilization", "0.10",
		"anthropic-ratelimit-unified-7d_oi-reset", itoa(now+86400),
		"anthropic-ratelimit-unified-14d-utilization", "0.2",
		"anthropic-ratelimit-unified-14d-reset", itoa(now+86400*9),
		"anthropic-ratelimit-unified-overage-status", "rejected",
	)
	ws := ClaudeWindows(h)
	ids := map[string]Window{}
	for _, w := range ws {
		ids[w.ID] = w
	}
	for _, want := range []string{"claude-5h", "claude-7d", "claude-7d_oi", "claude-14d", "claude-overage"} {
		if _, ok := ids[want]; !ok {
			t.Fatalf("missing %s in %+v", want, ws)
		}
	}
	if u := *ids["claude-5h"].UsedPercent; u < 61.9 || u > 62.1 {
		t.Fatalf("5h used %v", u)
	}
	if !ids["claude-overage"].Soft || ids["claude-overage"].Spent(time.Now().UnixMilli()) {
		t.Fatal("overage must be soft and never block")
	}
	if ws[0].ID != "claude-5h" {
		t.Fatalf("shortest first, got %s", ws[0].ID)
	}
	if ids["claude-14d"].Label != "14 days" {
		t.Fatalf("label %q", ids["claude-14d"].Label)
	}
}

func TestCodexWindowsIncludeOtherLimits(t *testing.T) {
	now := time.Now().Unix()
	h := hdr(
		"x-codex-primary-used-percent", "41",
		"x-codex-primary-window-minutes", "300",
		"x-codex-primary-reset-at", itoa(now+5000),
		"x-codex-secondary-used-percent", "72",
		"x-codex-secondary-window-minutes", "10080",
		"x-codex-secondary-reset-at", itoa(now+90000),
		"x-codex-bengalfox-primary-used-percent", "100",
		"x-codex-bengalfox-primary-window-minutes", "300",
		"x-codex-bengalfox-primary-reset-at", itoa(now+600),
	)
	ws := CodexWindows(h)
	if len(ws) != 3 {
		t.Fatalf("want 3 windows, got %+v", ws)
	}
	labels := map[string]string{}
	for _, w := range ws {
		labels[w.ID] = w.Label
	}
	if labels["codex-primary"] != "5 hours" || labels["codex-secondary"] != "7 days" || labels["codex-bengalfox-primary"] != "bengalfox, 5 hours" {
		t.Fatalf("labels %+v", labels)
	}
}

func TestLimits(t *testing.T) {
	now := time.Now().UnixMilli()
	reset := now/1000 + 3000
	lim := ClaudeLimit(hdr(
		"anthropic-ratelimit-unified-status", "rejected",
		"anthropic-ratelimit-unified-5h-status", "rejected",
		"anthropic-ratelimit-unified-5h-utilization", "1.0",
		"anthropic-ratelimit-unified-5h-reset", itoa(reset),
	), now)
	if !lim.Account || lim.Until != reset*1000 || lim.Label != "5 hours" {
		t.Fatalf("claude limit %+v", lim)
	}
	if far := ClaudeLimit(hdr("anthropic-ratelimit-unified-status", "rejected",
		"anthropic-ratelimit-unified-7d-status", "rejected",
		"anthropic-ratelimit-unified-7d-reset", itoa(now/1000+30*86400)), now); far.Until != 0 {
		t.Fatal("billing-boundary resets must be ignored")
	}
	body := []byte(`{"error":{"type":"usage_limit_reached","resets_at":` + itoa(reset) + `,"limit_window_minutes":10080}}`)
	cl := CodexLimit(nil, body, now)
	if !cl.Account || cl.Until != reset*1000 || cl.Label != "7 days" {
		t.Fatalf("codex limit %+v", cl)
	}
	if ra := RetryAfter(hdr("Retry-After", "20"), now); ra != now+20000 {
		t.Fatalf("retry-after %d", ra)
	}
}

func cred(id string, created int64) domain.Credential {
	return domain.Credential{ID: id, Provider: domain.ProviderClaude, CreatedAt: created}
}

func win(id string, period, resetIn int64, used float64, now int64) Window {
	r := now + resetIn
	return Window{ID: id, Label: id, UsedPercent: &used, ResetsAt: &r, PeriodMS: period}
}

func TestOrderSoonestResetFirstAndAffinity(t *testing.T) {
	e, _ := New(nil)
	now := time.Now().UnixMilli()
	e.now = func() int64 { return now }
	creds := []domain.Credential{cred("a", 1), cred("b", 2), cred("c", 3), cred("d", 4), cred("e", 5)}
	e.ObserveWindows("a", "headers", []Window{win("w", 7*dayMS, 4*dayMS, 20, now)})
	e.ObserveWindows("b", "headers", []Window{win("w", 7*dayMS, 1*dayMS, 70, now)})
	e.ObserveWindows("c", "headers", []Window{win("w", 7*dayMS, 2*dayMS, 100, now)}) // spent
	e.SetBenched("d", true)
	order, back := e.Order(domain.ProviderClaude, creds, "")
	got := ids(order)
	if got != "b,a,e" {
		t.Fatalf("order %s", got)
	}
	if back != now+2*dayMS {
		t.Fatalf("backAt %d", back)
	}
	e.Bind(domain.ProviderClaude, "s1", "a")
	order, _ = e.Order(domain.ProviderClaude, creds, "s1")
	if ids(order) != "a,b,e" || !order[0].Bound {
		t.Fatalf("affinity order %s", ids(order))
	}
	if moved := e.Bind(domain.ProviderClaude, "s1", "b"); moved != "a" {
		t.Fatalf("moved %q", moved)
	}
}

func TestCooldownAndRecovery(t *testing.T) {
	e, _ := New(nil)
	now := time.Now().UnixMilli()
	e.now = func() int64 { return now }
	creds := []domain.Credential{cred("a", 1), cred("b", 2)}
	e.RateLimited("a", Limit{Until: now + 60_000, Label: "5 hours", Account: true})
	if o, _ := e.Order(domain.ProviderClaude, creds, ""); ids(o) != "b" {
		t.Fatalf("cooling a must be skipped: %s", ids(o))
	}
	if st := e.Get("a"); st.CoolWindow != "5 hours" {
		t.Fatalf("cool window %q", st.CoolWindow)
	}
	now += 61_000
	if o, _ := e.Order(domain.ProviderClaude, creds, ""); len(o) != 2 {
		t.Fatal("cooldown should expire")
	}
	// Unknown 429: exponential backoff.
	b1 := e.RateLimited("b", Limit{})
	b2 := e.RateLimited("b", Limit{})
	if b1 != now+30_000 || b2 != now+60_000 {
		t.Fatalf("backoff %d %d", b1-now, b2-now)
	}
	e.Succeeded("b")
	if e.Get("b").Cooling(now) {
		t.Fatal("success clears backoff cooldown")
	}
	e.MarkSignIn("a", "refresh rejected")
	if o, _ := e.Order(domain.ProviderClaude, creds, ""); ids(o) != "b" {
		t.Fatal("needs sign-in must be skipped")
	}
	e.SignedIn("a")
	if o, _ := e.Order(domain.ProviderClaude, creds, ""); len(o) != 2 {
		t.Fatal("sign-in clears")
	}
}

func TestSessionKey(t *testing.T) {
	cases := []struct {
		h    map[string]string
		b    map[string]any
		want string
	}{
		{map[string]string{"X-Claude-Code-Session-Id": "abc"}, nil, "claude:abc"},
		{nil, map[string]any{"metadata": map[string]any{"user_id": `{"device_id":"d","session_id":"s1"}`}}, "claude:s1"},
		{nil, map[string]any{"metadata": map[string]any{"user_id": "user_x_account_y_session_0f0e8e9a-1111-2222-3333-444455556666"}}, "claude:0f0e8e9a-1111-2222-3333-444455556666"},
		{map[string]string{"session-id": "t1", "x-codex-parent-thread-id": "p1"}, nil, "codex:p1"},
		{map[string]string{"session-id": "t1"}, nil, "codex:t1"},
		{nil, map[string]any{"prompt_cache_key": "k"}, "pck:k"},
		{nil, map[string]any{}, "key:lk"},
	}
	for _, c := range cases {
		if c.b == nil {
			c.b = map[string]any{}
		}
		if got := SessionKey(c.h, c.b, "lk"); got != c.want {
			t.Fatalf("got %q want %q", got, c.want)
		}
	}
}

func ids(c []Candidate) string {
	s := ""
	for i, x := range c {
		if i > 0 {
			s += ","
		}
		s += x.Credential.ID
	}
	return s
}

func itoa(n int64) string {
	return strconvFormat(n)
}

func strconvFormat(n int64) string { return strconv.FormatInt(n, 10) }
