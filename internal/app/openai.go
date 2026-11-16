package app

import (
	"net/http"
	"strings"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
	"github.com/kacperkwapisz/mainspring/internal/session"
)

func registerOpenAI(mux *httpx.Mux, hub *proxy.Hub) {
	mux.Handle(http.MethodGet, "/v1/models", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, hub)
		if key == nil {
			writeUnauthorized(w, r, string(domain.InboundOpenAIChat))
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"object": "list", "data": servedModelsWithAPI(hub)})
	})
	mux.Handle(http.MethodPost, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, hub)
		if key == nil {
			writeUnauthorized(w, r, string(domain.InboundOpenAIChat))
			return
		}
		body, err := httpx.ParseBody(r)
		if err != nil {
			writeParseError(w, r, string(domain.InboundOpenAIChat), err)
			return
		}
		payload, _ := body.(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		result, err := proxy.Proxy(r.Context(), hub, proxy.Request{
			Inbound: domain.InboundOpenAIChat, Body: payload, Key: *key,
			Stream: payload["stream"] == true, InboundHeaders: httpx.InboundHeaders(r),
		})
		if err != nil {
			writeProxyErr(w, r, string(domain.InboundOpenAIChat), err)
			return
		}
		writeProxy(w, r, result)
	})
}

// Names of the endpoints a client can speak to Mainspring, as pi calls them.
const (
	apiMessages    = "anthropic-messages"
	apiResponses   = "openai-responses"
	apiCompletions = "openai-completions"
)

// modelWithAPI is a /v1/models row plus the endpoint that keeps the model's
// own features (caching, reasoning items) when a client calls it.
type modelWithAPI struct {
	domain.ModelInfo
	API string `json:"api"`
}

func servedModelsWithAPI(hub *proxy.Hub) []modelWithAPI {
	models := proxy.ServedModels(hub)
	out := make([]modelWithAPI, len(models))
	for i, m := range models {
		out[i] = modelWithAPI{ModelInfo: m, API: modelAPI(hub, m)}
	}
	return out
}

// modelAPI picks the Mainspring endpoint closest to the wire format the
// model's provider speaks upstream. Chat completions is the fallback, and what
// Cursor and OpenAI-compatible sources speak.
func modelAPI(hub *proxy.Hub, m domain.ModelInfo) string {
	// Only the built-in providers list unprefixed IDs under their own name. A
	// source's owned_by comes from that source, so don't trust it for a prefixed ID.
	switch m.OwnedBy {
	case string(domain.ProviderClaude):
		if !strings.Contains(m.ID, "/") {
			return apiMessages
		}
	case string(domain.ProviderCodex), string(domain.ProviderGrok):
		if !strings.Contains(m.ID, "/") {
			return apiResponses
		}
	case string(domain.ProviderOpenCode):
		// OpenCode serves all three wire formats, picked per model.
		if ex := hub.Executors[domain.ProviderOpenCode]; ex != nil && strings.HasPrefix(m.ID, "opencode/") {
			switch provider.FormatFor(ex, m.ID) {
			case domain.FormatClaude:
				return apiMessages
			case domain.FormatCodex:
				return apiResponses
			}
		}
	}
	return apiCompletions
}

func requireLocalKey(r *http.Request, hub *proxy.Hub) *domain.LocalKey {
	secret := session.BearerLocalKey(r)
	if secret == "" {
		return nil
	}
	key, _ := hub.Keys.Verify(secret)
	return key
}
