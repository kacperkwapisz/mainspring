package proxy

import (
	"context"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/translate"
)

// requestHints tells the translator what the hop's model accepts. Only
// OpenAI-format requests to Claude need it: they carry a reasoning effort that
// has to become Claude's adaptive thinking when the model takes efforts.
func requestHints(ctx context.Context, hub *Hub, ex provider.Executor, inbound domain.InboundFormat, providerID domain.ProviderID, model string) translate.RequestHints {
	if providerID != domain.ProviderClaude || inbound == domain.InboundClaudeMessages {
		return translate.RequestHints{}
	}
	return translate.RequestHints{Efforts: claudeEfforts(ctx, hub, ex, model)}
}

// claudeEfforts reads the model's effort levels from the accounts' own model
// lists (cached per account). It returns nil when no list names the model, and
// the request then keeps budget thinking.
func claudeEfforts(ctx context.Context, hub *Hub, ex provider.Executor, model string) []string {
	live, ok := ex.(liveModels)
	if !ok {
		return nil
	}
	creds, _ := hub.Vault.List(domain.ProviderClaude)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, c := range creds {
		if hub.Router != nil && hub.Router.Get(c.ID).NeedsSignIn {
			continue
		}
		for _, m := range live.ModelsFor(ctx, c) {
			if m.ID == model {
				return m.Efforts
			}
		}
	}
	return nil
}
