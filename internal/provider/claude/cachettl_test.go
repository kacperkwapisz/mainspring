package claude

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/translate"
)

func eph() map[string]any { return map[string]any{"type": "ephemeral"} }

func TestSetCacheTTLGivesEveryBreakpointAnHour(t *testing.T) {
	body := map[string]any{
		"tools":  []any{map[string]any{"name": "t", "cache_control": eph()}},
		"system": []any{map[string]any{"type": "text", "text": "s", "cache_control": eph()}},
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "a"},
			map[string]any{"type": "text", "text": "b", "cache_control": eph()},
		}}},
	}
	setCacheTTL(body, "1h")
	for _, cc := range []any{
		translate.AsMap(translate.AsArr(body["tools"])[0])["cache_control"],
		translate.AsMap(translate.AsArr(body["system"])[0])["cache_control"],
		translate.AsMap(translate.AsArr(translate.AsMap(translate.AsArr(body["messages"])[0])["content"])[1])["cache_control"],
	} {
		if translate.AsMap(cc)["ttl"] != "1h" {
			t.Fatalf("%v", cc)
		}
	}
	if _, has := translate.AsMap(translate.AsArr(translate.AsMap(translate.AsArr(body["messages"])[0])["content"])[0])["cache_control"]; has {
		t.Fatal("a block without a breakpoint gained one")
	}
}

// A client that names a TTL anywhere chose its caching; mixing would also be
// refused, since a one-hour breakpoint can't follow a five-minute one.
func TestSetCacheTTLLeavesAClientsOwnChoice(t *testing.T) {
	body := map[string]any{
		"system":   []any{map[string]any{"type": "text", "text": "s", "cache_control": map[string]any{"type": "ephemeral", "ttl": "5m"}}},
		"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "b", "cache_control": eph()}}}},
	}
	setCacheTTL(body, "1h")
	if _, has := translate.AsMap(translate.AsMap(translate.AsArr(translate.AsMap(translate.AsArr(body["messages"])[0])["content"])[0])["cache_control"])["ttl"]; has {
		t.Fatal("changed a body whose client named a TTL")
	}
}

// The hour is part of the signed body Claude receives.
func TestPrepareUpstreamSendsTheHour(t *testing.T) {
	body := map[string]any{"model": "claude-sonnet-5", "max_tokens": 10,
		"system":   []any{map[string]any{"type": "text", "text": "s", "cache_control": eph()}},
		"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	p := prepareUpstream(body, "tok", "cred", "", map[string]string{"User-Agent": "curl/8"}, false, false, "k", "1h")
	found := false
	for _, s := range translate.AsArr(translate.AsMap(p.Body)["system"]) {
		if translate.AsMap(translate.AsMap(s)["cache_control"])["ttl"] == "1h" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%v", p.Body)
	}
	if !strings.Contains(p.Headers["anthropic-beta"]+p.Headers["Anthropic-Beta"], "extended-cache-ttl") {
		t.Fatalf("beta header %v", p.Headers)
	}
	if q := PrepareUpstream(body, "tok", "cred", "", map[string]string{"User-Agent": "curl/8"}, false, false, "k"); strings.Contains(fmt.Sprint(q.Body), "ttl:1h") {
		t.Fatal("without a TTL the body must stay as it was")
	}
}

func countBreakpoints(v any) int {
	n := 0
	switch t := v.(type) {
	case map[string]any:
		if _, ok := t["cache_control"]; ok {
			n++
		}
		for _, x := range t {
			n += countBreakpoints(x)
		}
	case []any:
		for _, x := range t {
			n += countBreakpoints(x)
		}
	}
	return n
}

// A Claude Code request that isn't recognised as Claude Code (no claude-code
// beta) already carries four breakpoints; reshaping must not push it past
// Claude's limit of four.
func TestReshapedRequestStaysWithinFourBreakpoints(t *testing.T) {
	body := map[string]any{"model": "claude-sonnet-5", "max_tokens": 10,
		"system": []any{
			map[string]any{"type": "text", "text": "a"},
			map[string]any{"type": "text", "text": "b", "cache_control": eph()},
			map[string]any{"type": "text", "text": "c", "cache_control": eph()},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "1"}}},
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "2", "cache_control": eph()}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "3", "cache_control": eph()}}},
		}}
	for _, h := range []map[string]string{
		{"User-Agent": "claude-cli/2.1.322 (external, cli)", "x-app": "cli", "anthropic-beta": "oauth-2025-04-20"},
		{"User-Agent": "curl/8"},
	} {
		for _, ttl := range []string{"", "1h"} {
			p := prepareUpstream(translate.CloneJSON(body), "tok", "cred", "", h, false, false, "k", ttl)
			if n := countBreakpoints(p.Body); n > maxBreakpoints {
				t.Fatalf("%v ttl %q: %d breakpoints", h, ttl, n)
			}
			// The newest message keeps its breakpoint: it caches the whole conversation.
			msgs := translate.AsArr(translate.AsMap(p.Body)["messages"])
			last := translate.AsArr(translate.AsMap(msgs[len(msgs)-1])["content"])
			if _, ok := translate.AsMap(last[len(last)-1])["cache_control"]; !ok {
				t.Fatalf("%v: lost the last message's breakpoint", h)
			}
		}
	}
}
