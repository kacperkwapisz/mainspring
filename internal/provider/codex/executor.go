package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/translate"
)

const (
	api      = "https://chatgpt.com/backend-api/codex"
	tokenURL = "https://auth.openai.com/oauth/token"
)

type Executor struct{ ClientID string }

func (e Executor) ID() domain.ProviderID         { return domain.ProviderCodex }
func (e Executor) Format() domain.ExecutorFormat { return domain.FormatCodex }
func (e Executor) Models() []domain.ModelInfo    { return provider.CatalogModels(domain.ProviderCodex) }

func (e Executor) Execute(ctx context.Context, credential domain.Credential, body any, opts provider.ExecuteOptions) (provider.ExecuteResult, error) {
	sanitized := SanitizeBody(body, opts.InboundHeaders)
	if opts.Compact {
		sanitized = SanitizeCompactBody(body, opts.InboundHeaders)
	}
	cacheKey, _ := sanitized["prompt_cache_key"].(string)
	path := "/responses"
	if opts.Compact {
		path = "/responses/compact"
	}
	upstreamStream := !opts.Compact
	headers := UpstreamHeaders(credential.Tokens.AccessToken, accountID(credential), opts.InboundHeaders, upstreamStream, cacheKey)
	res, err := provider.PostJSON(ctx, api+path, sanitized, headers)
	if err != nil {
		return provider.FailFromErr(err), nil
	}
	return wrap(res, opts.Stream && upstreamStream)
}

func (e Executor) Refresh(ctx context.Context, credential domain.Credential) (domain.Credential, error) {
	if credential.Tokens.RefreshToken == "" {
		return credential, nil
	}
	form := url.Values{
		"client_id": {e.ClientID}, "grant_type": {"refresh_token"},
		"refresh_token": {credential.Tokens.RefreshToken}, "scope": {"openid email profile offline_access"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return credential, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	res, err := httpx.Client().Do(req)
	if err != nil {
		return credential, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return credential, &provider.RefreshError{Provider: domain.ProviderCodex, Status: res.StatusCode}
	}
	var jsonMap map[string]any
	_ = json.Unmarshal(raw, &jsonMap)
	credential.Tokens.AccessToken = translate.AsStr(jsonMap["access_token"], credential.Tokens.AccessToken)
	if r := translate.AsStr(jsonMap["refresh_token"]); r != "" {
		credential.Tokens.RefreshToken = r
	}
	if n, ok := jsonMap["expires_in"].(float64); ok {
		v := time.Now().UnixMilli() + int64(n)*1000
		credential.ExpiresAt = &v
	}
	credential.UpdatedAt = time.Now().UnixMilli()
	return credential, nil
}

func accountID(credential domain.Credential) string {
	if credential.Tokens.AccountID != "" {
		return credential.Tokens.AccountID
	}
	return chatgptAccountID(credential.Tokens.AccessToken, extraString(credential.Tokens.Extra, "id_token"))
}

func extraString(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	s, _ := extra[key].(string)
	return s
}

func wrap(res *http.Response, stream bool) (provider.ExecuteResult, error) {
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return provider.FailFromResponse(res), nil
	}
	if stream {
		ch := make(chan any, 16)
		go provider.ParseSSE(res.Body, ch)
		return provider.ExecuteResult{OK: true, Status: res.StatusCode, Stream: ch, Header: res.Header}, nil
	}
	defer res.Body.Close()
	ch := make(chan any, 16)
	go provider.ParseSSE(res.Body, ch)
	body := collectCodexResponse(ch)
	return provider.ExecuteResult{OK: true, Status: res.StatusCode, Body: body, Header: res.Header}, nil
}

// collectCodexResponse turns Codex's event stream into one response. The final
// response.completed event carries an empty output; the items themselves
// arrive as response.output_item.done events, so they're gathered here.
func collectCodexResponse(ch <-chan any) any {
	var last any = map[string]any{"object": "response", "status": "completed", "output": []any{}}
	items := map[int]any{}
	order := []int{}
	for chunk := range ch {
		ev := translate.AsMap(chunk)
		typ := translate.AsStr(ev["type"])
		switch typ {
		case "response.output_item.done":
			idx := len(order)
			if n, ok := ev["output_index"].(float64); ok {
				idx = int(n)
			}
			if _, seen := items[idx]; !seen {
				order = append(order, idx)
			}
			items[idx] = ev["item"]
		case "response.completed", "response.done", "response.incomplete", "response.failed":
			if resp := translate.AsMap(ev["response"]); resp != nil {
				if len(translate.AsArr(resp["output"])) == 0 && len(order) > 0 {
					sort.Ints(order)
					out := make([]any, 0, len(order))
					for _, i := range order {
						out = append(out, items[i])
					}
					resp["output"] = out
				}
				return resp
			}
		case "error":
			return ev
		}
		last = chunk
	}
	return last
}
