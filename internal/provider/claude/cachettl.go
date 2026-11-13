package claude

import "github.com/kacperkwapisz/mainspring/internal/translate"

// setCacheTTL gives every cache breakpoint without a TTL the given one. Claude
// Code asks for an hour on a subscription but five minutes behind a custom
// base URL, and other clients rarely ask at all, so a session that pauses for
// more than five minutes would write its whole prompt cache again.
//
// A body that already names a TTL anywhere is left as it is: the client chose,
// and Claude refuses a one-hour breakpoint after a five-minute one.
func setCacheTTL(body map[string]any, ttl string) {
	var marks []map[string]any
	explicit := false
	visit := func(block any) {
		b, ok := block.(map[string]any)
		if !ok {
			return
		}
		cc, ok := b["cache_control"].(map[string]any)
		if !ok {
			return
		}
		if _, has := cc["ttl"]; has {
			explicit = true
			return
		}
		marks = append(marks, cc)
	}
	for _, t := range translate.AsArr(body["tools"]) {
		visit(t)
	}
	for _, s := range translate.AsArr(body["system"]) {
		visit(s)
	}
	for _, m := range translate.AsArr(body["messages"]) {
		for _, c := range translate.AsArr(translate.AsMap(m)["content"]) {
			visit(c)
		}
	}
	if explicit {
		return
	}
	for _, cc := range marks {
		cc["ttl"] = ttl
	}
}

// maxBreakpoints is how many cache_control blocks Claude accepts in a request.
const maxBreakpoints = 4

// capBreakpoints keeps a request within Claude's four breakpoints. A client
// like Claude Code already uses all four, and reshaping a request that isn't
// recognised as Claude Code adds its own, so Claude would refuse it with
// "A maximum of 4 blocks with cache_control may be provided".
//
// Breakpoints are read in the order Claude caches them (tools, system,
// messages). The first two cover the stable start (identity, system, tools)
// and the last two the conversation so far, so those stay and the ones in
// between go: their prefix is covered by the next breakpoint kept.
func capBreakpoints(body map[string]any) {
	var marks []map[string]any
	visit := func(block any) {
		if b, ok := block.(map[string]any); ok {
			if _, has := b["cache_control"]; has {
				marks = append(marks, b)
			}
		}
	}
	for _, t := range translate.AsArr(body["tools"]) {
		visit(t)
	}
	for _, s := range translate.AsArr(body["system"]) {
		visit(s)
	}
	for _, m := range translate.AsArr(body["messages"]) {
		for _, c := range translate.AsArr(translate.AsMap(m)["content"]) {
			visit(c)
		}
	}
	if len(marks) <= maxBreakpoints {
		return
	}
	for _, b := range marks[maxBreakpoints/2 : len(marks)-maxBreakpoints/2] {
		delete(b, "cache_control")
	}
}
