package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

// webFakeExec is a Claude executor that records which account ran a search.
type webFakeExec struct {
	mu   sync.Mutex
	used []string
}

func (f *webFakeExec) ID() domain.ProviderID         { return domain.ProviderClaude }
func (f *webFakeExec) Format() domain.ExecutorFormat { return domain.FormatClaude }
func (f *webFakeExec) Models() []domain.ModelInfo {
	return []domain.ModelInfo{{ID: "claude-haiku-5-5"}}
}
func (f *webFakeExec) Refresh(_ context.Context, c domain.Credential) (domain.Credential, error) {
	return c, nil
}
func (f *webFakeExec) Execute(context.Context, domain.Credential, any, provider.ExecuteOptions) (provider.ExecuteResult, error) {
	return provider.ExecuteResult{OK: false, Status: 500}, nil
}
func (f *webFakeExec) WebSearch(_ context.Context, c domain.Credential, model, query string, _, _ []string) (provider.WebAnswer, provider.ExecuteResult) {
	f.mu.Lock()
	f.used = append(f.used, c.Label)
	f.mu.Unlock()
	return provider.WebAnswer{Text: "found " + query}, provider.ExecuteResult{OK: true, Status: 200}
}
func (f *webFakeExec) Ask(ctx context.Context, c domain.Credential, model, _, prompt string) (provider.WebAnswer, provider.ExecuteResult) {
	return f.WebSearch(ctx, c, model, prompt, nil, nil)
}

func TestMCPWebToolsRunOnTheCallingSessionsAccount(t *testing.T) {
	b := bootAPI(t)
	exec := &webFakeExec{}
	b.Hub.Executors[domain.ProviderClaude] = exec
	created, err := b.Hub.Keys.Create("laptop", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := b.Hub.Keys.Verify(created.Secret)
	// Three pi sessions on one key, each bound to its own account.
	for _, label := range []string{"a", "b", "c"} {
		c, err := b.Hub.Vault.Save(store.SaveCredential{Provider: domain.ProviderClaude, Label: label, Tokens: domain.CredentialTokens{AccessToken: "t"}})
		if err != nil {
			t.Fatal(err)
		}
		sk := route.SessionKey(map[string]string{"X-Session-ID": "pi-" + label}, nil, key.ID)
		b.Hub.Router.Bind(domain.ProviderClaude, sk, c.ID)
		b.Hub.Router.Touch(route.Touch{Provider: domain.ProviderClaude, SessionKey: sk, CredID: c.ID, LocalKeyID: key.ID, Model: "claude-opus-5-5"})
		time.Sleep(3 * time.Millisecond)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"web_search","arguments":{"query":"x"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Secret)
	req.Header.Set("X-Session-ID", "pi-b")
	res := httptest.NewRecorder()
	b.Handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d: %s", res.Code, res.Body)
	}
	if len(exec.used) != 1 || exec.used[0] != "b" {
		t.Fatalf("want the search on session pi-b's account b, got %v (%s)", exec.used, res.Body)
	}
}
