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
