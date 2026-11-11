---
title: Web tools
summary: web_search and web_fetch over MCP, running on the Claude or Codex account your session already uses.
---

Mainspring is an MCP server at `/mcp` with two tools. Any app that supports remote MCP servers can use them with a Mainspring key.

- **`web_search`**: searches the web and returns a short summary with its sources. Optional `allowed_domains` and `blocked_domains`.
- **`web_fetch`**: reads one public web page. With a `prompt`, a model on your subscription answers from the page; without one, you get the page's text.

## Which account runs a call

The account your app's current Claude or Codex session uses, with the same model, so a search costs the same subscription as the work around it. With no session to follow, the router picks the account, using a small model (Haiku on Claude). If that account is out, the call moves to the next one.

Claude searches the way Claude Code does, in its own small request with the `web_search` server tool. Codex uses its built-in `web_search`. Calls update limits, and the **Web tools** card on the Meter counts searches and page questions by provider.

`web_fetch` downloads the page from Mainspring itself and refuses addresses on your local network, loopback, link-local and Tailscale ranges.

## Setup

Set `MAINSPRING_KEY` to a key from **Keys**, then add the server. **Settings → Web tools** shows these lines with your address filled in.

```sh
claude mcp add --transport http mainspring https://mainspring.your-tailnet.ts.net/mcp --header "Authorization: Bearer $MAINSPRING_KEY"
```

```toml
# ~/.codex/config.toml
[mcp_servers.mainspring]
url = "https://mainspring.your-tailnet.ts.net/mcp"
bearer_token_env_var = "MAINSPRING_KEY"
```

```sh
pi mcp add mainspring --url https://mainspring.your-tailnet.ts.net/mcp --bearer-token-env-var MAINSPRING_KEY
```

Turn the tools off under **Settings → Web tools**; `/mcp` then answers `404`.
