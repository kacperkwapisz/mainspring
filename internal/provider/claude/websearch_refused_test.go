package claude

import (
	"slices"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/provider"
)

func refusal(domains string) provider.ExecuteResult {
	return provider.ExecuteResult{OK: false, Status: 400, Message: "The following domains are not accessible to our user agent: [" + domains + "]. Read more"}
}

func TestWebSearchDropsDomainsClaudeRefuses(t *testing.T) {
	var calls [][]string
	_, res := searchAround([]string{"reddit.com", "news.ycombinator.com"}, nil, func(allowed, _ []string) (provider.WebAnswer, provider.ExecuteResult) {
		calls = append(calls, allowed)
		if slices.Contains(allowed, "reddit.com") {
			return provider.WebAnswer{}, refusal("'reddit.com'")
		}
		return provider.WebAnswer{Text: "ok"}, provider.ExecuteResult{OK: true, Status: 200}
	})
	if !res.OK || len(calls) != 2 || !slices.Equal(calls[1], []string{"news.ycombinator.com"}) {
		t.Fatalf("res=%+v calls=%v", res, calls)
	}

	calls = nil
	_, res = searchAround(nil, []string{"reddit.com"}, func(_, blocked []string) (provider.WebAnswer, provider.ExecuteResult) {
		calls = append(calls, blocked)
		if len(blocked) > 0 {
			return provider.WebAnswer{}, refusal("'reddit.com'")
		}
		return provider.WebAnswer{Text: "ok"}, provider.ExecuteResult{OK: true, Status: 200}
	})
	if !res.OK || len(calls) != 2 {
		t.Fatalf("blocked: res=%+v calls=%v", res, calls)
	}
}

func TestWebSearchOnlyOnRefusedDomainsTriesAnotherProvider(t *testing.T) {
	n := 0
	_, res := searchAround([]string{"reddit.com"}, nil, func(_, _ []string) (provider.WebAnswer, provider.ExecuteResult) {
		n++
		return provider.WebAnswer{}, refusal("'reddit.com'")
	})
	if res.OK || !res.Retryable || n != 1 || res.Message != "Claude's web search can't reach reddit.com" {
		t.Fatalf("n=%d res=%+v", n, res)
	}
}
