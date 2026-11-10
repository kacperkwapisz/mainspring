package oauth

import (
	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/env"
)

func Logins(e *env.Env) map[domain.ProviderID]ProviderLogin {
	return map[domain.ProviderID]ProviderLogin{
		domain.ProviderClaude: ClaudeLogin(e),
		domain.ProviderCodex:  CodexLogin(e),
		domain.ProviderGrok:   GrokLogin(e),
		domain.ProviderCursor: CursorLogin(e),
	}
}
