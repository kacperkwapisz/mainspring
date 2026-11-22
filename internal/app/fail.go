package app

import (
	"errors"
	"net/http"

	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
)

func writeUnauthorized(w http.ResponseWriter, r *http.Request, inbound string) {
	httpx.Fail{
		Kind: httpx.KindAuth, Status: http.StatusUnauthorized, Route: r.URL.Path,
		Message: "invalid api key", Hint: "send Authorization: Bearer sk-ms-… from the panel",
	}.Log()
	if inbound == httpx.InboundClaude {
		httpx.ClaudeUnauthorized(w)
		return
	}
	httpx.OpenAIUnauthorized(w)
}

func writeParseError(w http.ResponseWriter, r *http.Request, inbound string, err error) {
	f := httpx.Fail{
		Kind: httpx.KindRequest, Status: 400, Type: "invalid_request_error",
		Route: r.URL.Path, Message: err.Error(), Hint: "send a JSON object",
	}
	var se *httpx.StatusError
	if errors.As(err, &se) && se.Status != 400 {
		f.Status, f.Hint, f.Type = se.Status, "", ""
		if se.Status == 499 {
			f.Kind = httpx.KindCancel
		}
	}
	f.Log()
	httpx.JSON(w, f.Status, f.ClientBody(inbound))
}

func writeProxyErr(w http.ResponseWriter, r *http.Request, inbound string, err error) {
	status, kind, msg := httpx.ClassifyErr(err)
	_, typ, message, hint := httpx.Humanize(status, "", msg)
	f := httpx.Fail{
		Kind: kind, Status: status, Type: typ, Route: r.URL.Path,
		Message: message, Hint: hint,
	}
	f.Log()
	httpx.JSON(w, status, f.ClientBody(inbound))
}

func writeProxy(w http.ResponseWriter, r *http.Request, result proxy.Result) {
	for k, vs := range result.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if !result.OK {
		httpx.JSON(w, result.Status, result.Body)
		return
	}
	if result.Stream != nil {
		httpx.StreamSSE(w, r, result.Stream)
		return
	}
	if result.Raw != nil {
		httpx.Bytes(w, http.StatusOK, "application/json; charset=utf-8", result.Raw)
		return
	}
	httpx.JSON(w, http.StatusOK, result.Body)
}
