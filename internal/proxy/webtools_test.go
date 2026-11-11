package proxy

import (
	"context"
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

type searchExec struct {
	fakeExec
	used   *[]string
	status map[string]int // per account label; 0 means OK
}

func (s searchExec) WebSearch(_ context.Context, c domain.Credential, model, query string, _, _ []string) (provider.WebAnswer, provider.ExecuteResult) {
	*s.used = append(*s.used, c.Label+"/"+model)
	if st := s.status[c.Label]; st != 0 {
		return provider.WebAnswer{}, provider.ExecuteResult{OK: false, Status: st, Retryable: st == 429}
	}
	a := provider.WebAnswer{Text: "found " + query}
	a.AddSource("Go", "https://go.dev")
	return a, provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{"usage": map[string]any{"input_tokens": 10.0, "output_tokens": 5.0}}}
}

func (s searchExec) Ask(ctx context.Context, c domain.Credential, model, _, prompt string) (provider.WebAnswer, provider.ExecuteResult) {
	return s.WebSearch(ctx, c, model, prompt, nil, nil)
}

func TestWebSearchFollowsTheCallersSessionAccount(t *testing.T) {
	var used []string
	ex := searchExec{fakeExec: fakeExec{id: domain.ProviderClaude, format: domain.FormatClaude, models: []domain.ModelInfo{{ID: "claude-haiku-5-5"}}}, used: &used, status: map[string]int{}}
	hub, d := testHub(t, map[domain.ProviderID]provider.Executor{domain.ProviderClaude: ex})
	defer d.Close()
	var ids []string
	for _, label := range []string{"a", "b", "c"} {
		c, _ := hub.Vault.Save(store.SaveCredential{Provider: domain.ProviderClaude, Label: label, Tokens: domain.CredentialTokens{AccessToken: "t"}})
		ids = append(ids, c.ID)
	}
	created, _ := hub.Keys.Create("laptop", nil, nil, nil)
	key, _ := hub.Keys.Verify(created.Secret)

	// The key is mid-session on account b with Opus.
	hub.Router.Bind(domain.ProviderClaude, "sess-1", ids[1])
	hub.Router.Touch(route.Touch{Provider: domain.ProviderClaude, SessionKey: "sess-1", CredID: ids[1], LocalKeyID: key.ID, Model: "claude-opus-5-5"})

	res, err := WebSearch(context.Background(), hub, *key, "go 1.26", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(used) != 1 || used[0] != "b/claude-opus-5-5" {
		t.Fatalf("want account b with the session's model, got %v", used)
	}
	if res.Text != "found go 1.26" || len(res.Sources) != 1 || res.Provider != domain.ProviderClaude {
		t.Fatalf("%+v", res)
	}

	// Account b is out: the search moves on instead of failing.
	used = nil
	ex.status["b"] = 429
	hub.Executors[domain.ProviderClaude] = ex
	if _, err := WebSearch(context.Background(), hub, *key, "again", nil, nil); err != nil || len(used) != 2 || !strings.HasPrefix(used[0], "b/") || strings.HasPrefix(used[1], "b/") {
		t.Fatalf("should try b, then another account: %v %v", used, err)
	}
}

func TestWebSearchWithoutASessionUsesTheRouterAndASmallModel(t *testing.T) {
	var used []string
	ex := searchExec{fakeExec: fakeExec{id: domain.ProviderClaude, format: domain.FormatClaude, models: []domain.ModelInfo{{ID: "claude-opus-5-5"}, {ID: "claude-haiku-5-5"}}}, used: &used, status: map[string]int{}}
	hub, d := testHub(t, map[domain.ProviderID]provider.Executor{domain.ProviderClaude: ex})
	defer d.Close()
	_, _ = hub.Vault.Save(store.SaveCredential{Provider: domain.ProviderClaude, Label: "a", Tokens: domain.CredentialTokens{AccessToken: "t"}})
	created, _ := hub.Keys.Create("laptop", nil, nil, nil)
	key, _ := hub.Keys.Verify(created.Secret)
	if _, err := WebSearch(context.Background(), hub, *key, "q", nil, nil); err != nil || len(used) != 1 || used[0] != "a/claude-haiku-5-5" {
		t.Fatalf("%v %v", used, err)
	}
	// A key limited to Codex can't use Claude for web tools either.
	limited, _ := hub.Keys.Create("codex-only", []domain.ProviderID{domain.ProviderCodex}, nil, nil)
	lk, _ := hub.Keys.Verify(limited.Secret)
	if _, err := WebSearch(context.Background(), hub, *lk, "q", nil, nil); err == nil {
		t.Fatal("a key without Claude must not search on Claude")
	}
}

func TestFetchPageRefusesPrivateAddresses(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:8317/api/pool", "http://192.168.1.10/", "http://[::1]/", "http://100.100.100.100/", "file:///etc/passwd"} {
		if _, _, err := FetchPage(context.Background(), u, 1000); err == nil {
			t.Errorf("%s should be refused", u)
		}
	}
}

func TestHTMLToTextKeepsReadableContent(t *testing.T) {
	title, text := htmlToText(`<html><head><title>Go 1.26</title><script>evil()</script><style>p{}</style></head>
	<body><nav>Home | Blog</nav><h1>Go 1.26 is released</h1><p>Released on <b>Feb 10</b>. See <a href="https://go.dev/doc">the notes</a>.</p>
	<ul><li>Green Tea GC</li><li>new(expr)</li></ul><pre><code>go install</code></pre><footer>©</footer></body></html>`)
	if title != "Go 1.26" {
		t.Fatalf("title %q", title)
	}
	for _, want := range []string{"# Go 1.26 is released", "Released on Feb 10", "[the notes](https://go.dev/doc)", "- Green Tea GC", "go install"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, gone := range []string{"evil", "Home | Blog", "©"} {
		if strings.Contains(text, gone) {
			t.Errorf("should drop %q", gone)
		}
	}
}
