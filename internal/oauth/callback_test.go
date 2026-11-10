package oauth

import (
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

func TestParseCallbackClaudeURL(t *testing.T) {
	p := ParseCallback("http://localhost:54545/callback?code=abc&state=xyz")
	if p.Code != "abc" || p.State != "xyz" {
		t.Fatalf("%+v", p)
	}
}

func TestParseCallbackCodexURL(t *testing.T) {
	p := ParseCallback("http://localhost:1455/auth/callback?code=tok")
	if p.Code != "tok" {
		t.Fatalf("%+v", p)
	}
}

func TestParseCallbackBareCode(t *testing.T) {
	p := ParseCallback("just-a-code")
	if p.Code != "just-a-code" {
		t.Fatalf("%+v", p)
	}
}

func TestParseCallbackErrorURL(t *testing.T) {
	p := ParseCallback("http://localhost:54545/callback?error=access_denied&error_description=nope")
	if p.Error != "access_denied" || p.ErrorDescription != "nope" {
		t.Fatalf("%+v", p)
	}
}

func TestTakePendingOneShot(t *testing.T) {
	now := time.Now().UnixMilli()
	PutPending(PendingLogin{Provider: domain.ProviderClaude, State: "s1", Verifier: "v", CreatedAt: now})
	PutPending(PendingLogin{Provider: domain.ProviderClaude, State: "s2", Verifier: "v2", CreatedAt: now + 1})
	first := TakePending("s1")
	if first == nil || first.State != "s1" {
		t.Fatalf("%+v", first)
	}
	if TakePending("s1") != nil {
		t.Fatal("not one-shot")
	}
	latest := TakePendingForProvider(domain.ProviderClaude)
	if latest == nil || latest.State != "s2" {
		t.Fatalf("%+v", latest)
	}
}

func TestParseCallbackCopyCode(t *testing.T) {
	got := ParseCallback("  abc123#st4te ")
	if got.Code != "abc123" || got.State != "st4te" {
		t.Fatalf("%+v", got)
	}
}

func TestRandomStateMatchesClaudeCode(t *testing.T) {
	if n := len(RandomState()); n != 43 {
		t.Fatalf("state length %d, Claude Code sends 43", n)
	}
}

func TestClaudeLoginResultReadsAccount(t *testing.T) {
	got, err := claudeLoginResult(map[string]any{
		"access_token": "at", "refresh_token": "rt", "expires_in": float64(3600),
		"account":      map[string]any{"uuid": "11111111-2222-4333-8444-555555555555", "email_address": "sam@example.com"},
		"organization": map[string]any{"uuid": "org-1", "name": "Example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "sam@example.com" || got.Tokens.Email != "sam@example.com" {
		t.Fatalf("email not read: label %q email %q", got.Label, got.Tokens.Email)
	}
	if got.Tokens.AccountID != "11111111-2222-4333-8444-555555555555" || got.Tokens.Extra["organization_uuid"] != "org-1" {
		t.Fatalf("ids: %q %v", got.Tokens.AccountID, got.Tokens.Extra)
	}
}
