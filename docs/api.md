---
title: API
summary: Every endpoint Mainspring answers, what it needs and what comes back, with the error format.
---

Mainspring speaks the OpenAI Chat Completions, OpenAI Responses and Anthropic Messages APIs, so most clients work by changing their base URL. Paths below are relative to the panel's address.

## Authentication

Send a Mainspring key as `Authorization: Bearer sk-ms-…` or `x-api-key: sk-ms-…`. Create keys under **Keys**. A missing or revoked key gets `401`.

## Errors

Errors come back in the format of the endpoint you called, with a message that says what happened and what to change.

OpenAI-style endpoints:

```json
{ "error": { "type": "rate_limit_exceeded", "code": "rate_limit_exceeded", "message": "every Claude account is resting …" } }
```

`/v1/messages` uses Anthropic's shape and types (`invalid_request_error`, `authentication_error`, `permission_error`, `not_found_error`, `rate_limit_error`, `api_error`):

```json
{ "error": { "type": "rate_limit_error", "message": "every Claude account is resting …" } }
```

| Status | Means |
|---|---|
| `400` | The request is malformed, or `model` is missing |
| `401` | No key, or a revoked one |
| `403` | The key isn't allowed this provider or model |
| `404` | Unknown or turned-off model |
| `429` | Every account of the provider is resting (`Retry-After` says until when), or the key's daily cap is used up |
| `502`, `503` | The provider failed, or no account of it is connected |

## POST /v1/messages

The Anthropic Messages API. Claude models go to your Claude accounts; other models are translated to their provider and back. `stream: true` returns Anthropic's server-sent events.

```sh
curl https://mainspring.your-tailnet.ts.net/v1/messages \
  -H "x-api-key: $MAINSPRING_KEY" \
  -H "content-type: application/json" \
  -d '{"model":"claude-sonnet-5-5","max_tokens":256,
       "messages":[{"role":"user","content":"Hi"}]}'
```

Session affinity follows Claude Code's session ID or `metadata.user_id`; see [How routing works](routing.md#sessions).

## POST /v1/messages/count_tokens

Counts a Messages request's input tokens without running it. Same body as `/v1/messages`.

## POST /v1/chat/completions

The OpenAI Chat Completions API, for every provider. `stream: true` returns OpenAI's server-sent events.

```sh
curl https://mainspring.your-tailnet.ts.net/v1/chat/completions \
  -H "Authorization: Bearer $MAINSPRING_KEY" \
  -H "content-type: application/json" \
  -d '{"model":"gpt-5.6-terra","messages":[{"role":"user","content":"Hi"}]}'
```

## POST /v1/responses

The OpenAI Responses API, which Codex uses. Codex headers such as its turn state pass through.

`GET /v1/responses` with a WebSocket upgrade serves the same API over WebSockets, as Codex does with `supports_websockets = true`. Turn it off under **Settings → Routing** if you need to.

## POST /v1/responses/compact

Codex's compaction endpoint, passed through to your Codex accounts.

## GET /v1/models

Every model your accounts can serve, minus the ones you turned off, in OpenAI's list format with extra fields:

```json
{
  "object": "list",
  "data": [
    {
      "id": "claude-sonnet-5-5",
      "object": "model",
      "owned_by": "claude",
      "name": "Claude Sonnet 5.5",
      "context_length": 1000000,
      "max_output_tokens": 128000,
      "efforts": ["low", "medium", "high", "xhigh", "max"]
    }
  ]
}
```

Needs a key.

## POST /mcp

An MCP server (Streamable HTTP, answered as plain JSON) with `web_search` and `web_fetch`. Needs a key. See [Web tools](web-tools.md).

## /v0/management

A small management API for usage dashboards such as T3 Code. It's off until you create a management key under **Settings → Usage hub**, and it takes that key, not an `sk-ms-` key, as `Authorization: Bearer …` or `X-Management-Key`.

| Endpoint | Does |
|---|---|
| `GET /v0/management/auth-files` | Lists the accounts with their limits |
| `POST /v0/management/api-call` | Calls a provider's usage endpoint as one account. Body: `auth_index`, `method`, `url`, optional `header` and `data` |
| `POST /v0/management/reset-quota` | Clears an account's cooldown. Body: `auth_index` |

`api-call` reaches only these URLs; anything else gets `403`:

- `GET https://api.anthropic.com/api/oauth/usage`
- `GET https://chatgpt.com/backend-api/wham/usage`
- `GET https://chatgpt.com/backend-api/wham/rate-limit-reset-credits`
- `POST https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume`, which spends one of the account's Codex reset credits

## GET /health

Answers `{"ok":true}`. Needs nothing.

## Docs

These docs are public:

| Path | Returns |
|---|---|
| `/docs`, `/docs/<page>` | A page, rendered |
| `/docs/<page>.md` | The page as Markdown |
| `/llms.txt` | An index of every page with a summary and its Markdown link |
| `/llms-full.txt` | Every page in one Markdown file |
