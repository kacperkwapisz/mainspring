---
title: Connect your tools
summary: Point Claude Code, Codex, Cursor, OpenCode, pi or any OpenAI- or Anthropic-style client at Mainspring.
---

Every tool needs two things: the endpoint and a key that starts with `sk-ms-`. The endpoint is the panel's address plus `/v1`, for example `https://mainspring.your-tailnet.ts.net/v1`. The panel shows it at the bottom of the sidebar. Create keys under [Keys](keys.md).

## Claude Code

Claude Code speaks the Anthropic API. Merge this into `~/.claude/settings.json`, using the address without `/v1`; Claude Code adds it:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://mainspring.your-tailnet.ts.net",
    "ANTHROPIC_AUTH_TOKEN": "sk-ms-…"
  }
}
```

Mainspring recognises Claude Code and keeps its session on one account, so the prompt cache stays warm.

## Codex

Add a provider to `~/.codex/config.toml`:

```toml
model_provider = "mainspring"

[model_providers.mainspring]
name = "Mainspring"
base_url = "https://mainspring.your-tailnet.ts.net/v1"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = true
experimental_bearer_token = "sk-ms-…"
```

`supports_websockets = true` makes turns faster. Turn it off in Codex if something between Codex and Mainspring drops WebSocket connections.

## Cursor

In Cursor's settings, open **Models**, turn on **OpenAI API Key**, paste your `sk-ms-` key and set **Override OpenAI Base URL** to the endpoint.

Cursor sends these requests from its own servers rather than from your machine, so the endpoint has to be reachable from the internet. A tailnet or LAN address won't work there. Cursor doesn't document this; it's what users and Cursor staff report on Cursor's forum.

## OpenCode

Add Mainspring as an OpenAI-compatible provider in `opencode.jsonc`, and set `MAINSPRING_KEY` to your key:

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "providers": {
    "mainspring": {
      "name": "Mainspring",
      "env": ["MAINSPRING_KEY"],
      "package": "@opencode/ai/providers/openai-compatible",
      "settings": { "baseURL": "https://mainspring.your-tailnet.ts.net/v1" },
      "models": {
        "claude-sonnet-5-5": { "name": "Claude Sonnet 5.5" },
        "gpt-5.6-terra": { "name": "GPT-5.6 Terra" }
      }
    }
  }
}
```

List the model IDs you want from [`/v1/models`](api.md#get-v1models).

## pi

Install the Mainspring extension. It adds Mainspring as a provider with the models your subscriptions offer, registers `web_search` and `web_fetch`, tells you when Mainspring moves your session to another account, and shows the pool with `/mainspring`:

```sh
pi install git:github.com/kacperkwapisz/mainspring
```

Then run `/mainspring setup` in pi and enter the address and a key, or set `MAINSPRING_URL` and `MAINSPRING_KEY`. Pick a model under **Mainspring** with `/model`.

Without the extension, add a provider to `~/.pi/agent/models.json`:

```json
{
  "providers": {
    "mainspring": {
      "baseUrl": "https://mainspring.your-tailnet.ts.net/v1",
      "api": "openai-completions",
      "apiKey": "$MAINSPRING_KEY",
      "models": [{ "id": "claude-sonnet-5-5" }, { "id": "gpt-5.6-terra" }]
    }
  }
}
```

## Anything else

Any client that speaks the OpenAI Chat Completions, OpenAI Responses or Anthropic Messages API works. Use the endpoint as its base URL and the `sk-ms-` key as its API key:

```text
Base URL   https://mainspring.your-tailnet.ts.net/v1
API key    sk-ms-…
```

Send the key as `Authorization: Bearer sk-ms-…` or as `x-api-key`.

## Web search and fetch

To give your tools web search that runs on your subscriptions, add Mainspring as an MCP server too. See [Web tools](web-tools.md).
