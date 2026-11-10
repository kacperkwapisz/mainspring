package oauth

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/env"
)

func TestCodexStartIncludesCLIQuery(t *testing.T) {
	login := CodexLogin(&env.Env{CodexClientID: env.EmbeddedCodexClientID})
	start, err := login.Start(t.Context(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"id_token_add_organizations=true",
		"codex_cli_simplified_flow=true",
		"originator=codex-tui",
	} {
		if !contains(start.URL, want) {
			t.Fatalf("missing %s in %s", want, start.URL)
		}
	}
}

func TestChatgptAccountIDPrefersAccessJWT(t *testing.T) {
	access := jwtWith(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-access"}}`)
	idTok := jwtWith(`{"email":"a@b.c"}`)
	if got := chatgptAccountID(access, idTok); got != "acct-access" {
		t.Fatalf("%s", got)
	}
}

func jwtWith(payload string) string {
	return "aaa." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"
}

func TestClaudeAuthorizeURL(t *testing.T) {
	e, _ := env.Load(map[string]string{"JWT_SECRET": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "DATABASE_PATH": ":memory:"})
	for mode, redirect := range map[string]string{"": ClaudeRedirectURI, "copy_code": ClaudeCopyCodeRedirectURI} {
		start, err := ClaudeLogin(e).Start(context.Background(), "", mode)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(start.URL)
		q := u.Query()
		if q.Get("code") != "true" || q.Get("redirect_uri") != redirect || !strings.HasPrefix(q.Get("scope"), "org:create_api_key ") || q.Get("code_challenge_method") != "S256" {
			t.Fatalf("mode %q: %s", mode, start.URL)
		}
	}
}
