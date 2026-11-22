package codex

import (
	"context"
	"net/http"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/provider"
)

// Codex generates and edits images through the ChatGPT account's own Images
// API, the one its built-in image tool calls. It takes the OpenAI Images
// API's JSON body (edits name their pictures in "images", as {image_url} or
// {file_id}) and answers in its format, with the pictures as b64_json.

// imageHeaders are the Codex headers its image tool sends that pass through.
var imageHeaders = []string{"X-Codex-Image-Turn-Id", "X-Codex-Turn-Metadata"}

// Images posts body to the Images API on this account. edit picks
// /images/edits over /images/generations. The response is returned as it
// came, so the caller can pass it on as is.
func (e Executor) Images(ctx context.Context, credential domain.Credential, edit bool, body map[string]any, inbound map[string]string) (*http.Response, error) {
	path := "/images/generations"
	if edit {
		path = "/images/edits"
	}
	headers := map[string]string{
		"Authorization": "Bearer " + credential.Tokens.AccessToken,
		"Accept":        "application/json",
		"Originator":    Originator,
		"User-Agent":    UserAgent,
	}
	if id := accountID(credential); id != "" {
		headers["ChatGPT-Account-Id"] = id
	}
	if body["stream"] == true {
		headers["Accept"] = "text/event-stream"
	}
	for _, name := range imageHeaders {
		if v := inboundHeader(inbound, name); v != "" {
			headers[name] = v
		}
	}
	return provider.PostJSON(ctx, api+path, body, headers)
}
