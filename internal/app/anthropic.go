package app

import (
	"net/http"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
)

func registerAnthropic(mux *httpx.Mux, hub *proxy.Hub) {
	mux.Handle(http.MethodPost, "/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, hub)
		if key == nil {
			writeUnauthorized(w, r, string(domain.InboundClaudeMessages))
			return
		}
		body, err := httpx.ParseBody(r)
		if err != nil {
			writeParseError(w, r, string(domain.InboundClaudeMessages), err)
			return
		}
		payload, _ := body.(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		result, err := proxy.Proxy(r.Context(), hub, proxy.Request{
			Inbound: domain.InboundClaudeMessages, Body: payload, Key: *key,
			Stream: payload["stream"] == true, InboundHeaders: httpx.InboundHeaders(r),
		})
		if err != nil {
			writeProxyErr(w, r, string(domain.InboundClaudeMessages), err)
			return
		}
		writeProxy(w, r, result)
	})
	mux.Handle(http.MethodPost, "/v1/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, hub)
		if key == nil {
			writeUnauthorized(w, r, string(domain.InboundClaudeMessages))
			return
		}
		body, err := httpx.ParseBody(r)
		if err != nil {
			writeParseError(w, r, string(domain.InboundClaudeMessages), err)
			return
		}
		payload, _ := body.(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		result, err := proxy.Proxy(r.Context(), hub, proxy.Request{
			Inbound: domain.InboundClaudeMessages, Body: payload, Key: *key,
			CountTokens: true, InboundHeaders: httpx.InboundHeaders(r),
		})
		if err != nil {
			writeProxyErr(w, r, string(domain.InboundClaudeMessages), err)
			return
		}
		writeProxy(w, r, result)
	})
}
