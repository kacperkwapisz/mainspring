package proxy

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

func okClaude() provider.ExecuteResult {
	return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{
		"id": "msg", "content": []any{map[string]any{"type": "text", "text": "ok"}},
		"stop_reason": "end_turn", "usage": map[string]any{"input_tokens": 1.0, "output_tokens": 1.0},
	}}
}

func claudeOnly(fn func(domain.Credential) provider.ExecuteResult, refresh func(domain.Credential) (domain.Credential, error)) map[domain.ProviderID]provider.Executor {
	return map[domain.ProviderID]provider.Executor{
		domain.ProviderClaude: fakeExec{
			id: domain.ProviderClaude, format: domain.FormatClaude,
			models: []domain.ModelInfo{{ID: "claude-opus-4-7", Object: "model", OwnedBy: "claude"}},
			fn:     fn, refresh: refresh,
		},
	}
}

func call(t *testing.T, hub *Hub, headers map[string]string) Result {
	t.Helper()
	created, err := hub.Keys.Create("t"+strconv.FormatInt(time.Now().UnixNano(), 36), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := hub.Keys.Verify(created.Secret)
	res, err := Proxy(context.Background(), hub, Request{
		Inbound:        domain.InboundOpenAIChat,
		Body:           map[string]any{"model": "claude-opus-4-7", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Key:            *key,
		InboundHeaders: headers,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func save(hub *Hub, id string) {
	_, _ = hub.Vault.Save(store.SaveCredential{ID: id, Provider: domain.ProviderClaude, Label: id + "@x", Tokens: domain.CredentialTokens{AccessToken: id, Extra: map[string]any{}}})
}

func TestLimitedAccountIsSkippedUntilReset(t *testing.T) {
	reset := time.Now().Add(time.Hour).Unix()
	var seen []string
	hub, d := testHub(t, claudeOnly(func(c domain.Credential) provider.ExecuteResult {
		seen = append(seen, c.ID)
		if c.ID == "a" {
			h := http.Header{}
			h.Set("anthropic-ratelimit-unified-status", "rejected")
			h.Set("anthropic-ratelimit-unified-5h-status", "rejected")
			h.Set("anthropic-ratelimit-unified-5h-utilization", "1.0")
			h.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset, 10))
			return provider.ExecuteResult{OK: false, Status: 429, Retryable: true, Header: h, Message: "limit"}
		}
		return okClaude()
	}, nil))
	defer d.Close()
	save(hub, "a")
	save(hub, "b")
	sess := map[string]string{"X-Claude-Code-Session-Id": "s1"}
	if res := call(t, hub, sess); !res.OK {
		t.Fatalf("%+v", res)
	}
	if res := call(t, hub, sess); !res.OK {
		t.Fatalf("%+v", res)
	}
	if got := len(seen); got != 3 || seen[2] != "b" {
		t.Fatalf("second request must skip the limited account: %v", seen)
	}
	st := hub.Router.Get("a")
	if st.CoolUntil != reset*1000 || st.CoolWindow != "5 hours" {
		t.Fatalf("cooldown %+v", st)
	}
	events, _ := hub.Activity.Since(0, 10)
	if len(events) == 0 || events[len(events)-1].Kind != store.ActivityLimit {
		t.Fatalf("activity %+v", events)
	}
}

func TestEveryAccountRestingReturns429WithRetryAfter(t *testing.T) {
	hub, d := testHub(t, claudeOnly(func(domain.Credential) provider.ExecuteResult { return okClaude() }, nil))
	defer d.Close()
	save(hub, "a")
	hub.Router.RateLimited("a", routeLimit(time.Now().Add(50*time.Minute).UnixMilli()))
	res := call(t, hub, nil)
	if res.OK || res.Status != 429 {
		t.Fatalf("%+v", res)
	}
	ra, _ := strconv.Atoi(res.Header.Get("Retry-After"))
	if ra < 2990 || ra > 3001 {
		t.Fatalf("retry-after %d", ra)
	}
}

func TestRejectedRefreshMarksNeedsSignIn(t *testing.T) {
	var seen []string
	hub, d := testHub(t, claudeOnly(func(c domain.Credential) provider.ExecuteResult {
		seen = append(seen, c.ID)
		if c.ID == "a" {
			return provider.ExecuteResult{OK: false, Status: 401, Retryable: true}
		}
		return okClaude()
	}, func(c domain.Credential) (domain.Credential, error) {
		if c.ID == "a" {
			return c, &provider.RefreshError{Provider: domain.ProviderClaude, Status: 400}
		}
		return c, nil
	}))
	defer d.Close()
	save(hub, "a")
	save(hub, "b")
	if res := call(t, hub, nil); !res.OK {
		t.Fatalf("%+v", res)
	}
	if !hub.Router.Get("a").NeedsSignIn {
		t.Fatal("a needs sign-in")
	}
	seen = nil
	_ = call(t, hub, nil)
	if len(seen) != 1 || seen[0] != "b" {
		t.Fatalf("needs-sign-in account must be skipped: %v", seen)
	}
}

func TestSessionsStickAndBenchedIsSkipped(t *testing.T) {
	var seen []string
	hub, d := testHub(t, claudeOnly(func(c domain.Credential) provider.ExecuteResult {
		seen = append(seen, c.ID)
		return okClaude()
	}, nil))
	defer d.Close()
	save(hub, "a")
	save(hub, "b")
	hub.Router.Bind(domain.ProviderClaude, "claude:s2", "b")
	for i := 0; i < 3; i++ {
		call(t, hub, map[string]string{"X-Claude-Code-Session-Id": "s2"})
	}
	if seen[0] != "b" || seen[1] != "b" || seen[2] != "b" {
		t.Fatalf("affinity: %v", seen)
	}
	hub.Router.SetBenched("b", true)
	seen = nil
	call(t, hub, map[string]string{"X-Claude-Code-Session-Id": "s2"})
	if seen[0] != "a" {
		t.Fatalf("benched skipped: %v", seen)
	}
	live := hub.Router.LiveSessions()
	if len(live) != 1 || live[0].CredID != "a" || live[0].MovedFrom != "b" || live[0].Requests != 4 {
		t.Fatalf("live %+v", live)
	}
}

func routeLimit(until int64) route.Limit {
	return route.Limit{Until: until, Account: true, Label: "5 hours"}
}
