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
