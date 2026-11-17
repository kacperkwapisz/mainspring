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

// clientBreakpoint tags, for the length of prepareUpstream, the blocks that
// carried a breakpoint before the disguise reshaped the request, so
// capBreakpoints can tell the client's breakpoints from the disguise's. It is
// a key on the block because the disguise copies blocks it keeps.
const clientBreakpoint = "\x00mainspring-client-breakpoint"

// markClientBreakpoints tags the client's breakpoints (see clientBreakpoint).
func markClientBreakpoints(body map[string]any) {
	eachBlock(body, func(b map[string]any) {
		if _, has := b["cache_control"]; has {
			b[clientBreakpoint] = true
		}
	})
}

// capBreakpoints makes room for the breakpoints the disguise added to a
// request that isn't recognised as Claude Code. Claude takes at most four, and
// a client like Claude Code already uses all four, so Claude would refuse the
// disguised request with "A maximum of 4 blocks with cache_control may be
// provided". It drops only as many of the client's breakpoints as the
// disguise added; a client that sends more than four itself gets Claude's
// error, as it would without Mainspring.
//
// Breakpoints are read in the order Claude caches them (tools, system,
// messages). The client's first and last breakpoints stay, the start of the
// prompt and the conversation so far, and the ones in between go: their
// prefix is covered by the next breakpoint kept.
func capBreakpoints(body map[string]any) {
	var client []map[string]any
	total := 0
	eachBlock(body, func(b map[string]any) {
		_, tagged := b[clientBreakpoint]
		delete(b, clientBreakpoint)
		if _, has := b["cache_control"]; !has {
			return
		}
		total++
		if tagged {
			client = append(client, b)
		}
	})
	drop := min(total-maxBreakpoints, total-len(client))
	if drop <= 0 {
		return
	}
	keep := len(client) - drop
	for _, b := range client[keep/2 : keep/2+drop] {
		delete(b, "cache_control")
	}
}

// eachBlock calls fn for every block that can carry a breakpoint, in the
// order Claude caches them: tools, system, then message content.
func eachBlock(body map[string]any, fn func(map[string]any)) {
	visit := func(block any) {
		if b, ok := block.(map[string]any); ok {
			fn(b)
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
}
