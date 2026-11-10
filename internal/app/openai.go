package app

import (
	"net/http"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
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
		httpx.JSON(w, http.StatusOK, map[string]any{"object": "list", "data": proxy.ServedModels(hub)})
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

func requireLocalKey(r *http.Request, hub *proxy.Hub) *domain.LocalKey {
	secret := session.BearerLocalKey(r)
	if secret == "" {
		return nil
	}
	key, _ := hub.Keys.Verify(secret)
	return key
}
