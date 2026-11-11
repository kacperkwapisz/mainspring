---
title: Your usage in other tools
summary: See your accounts' limits outside the panel, in pi, in T3 Code, or in your own scripts.
---

The panel shows every account's limits. Three ways show them elsewhere, each with its own credential.

| Way | Credential | Shows |
|---|---|---|
| [pi extension](#pi) | An `sk-ms-` key | Every provider |
| [T3 Code](#t3-code) | The management key | Claude and Codex accounts |
| [`/v1/pool` and response headers](#your-own-tools) | An `sk-ms-` key | Every provider |

The `sk-ms-` ways need **Share pool with keys** on, under **Settings → Keys**. It's on by default.

## pi

With the [Mainspring extension](connect.md#pi), `/mainspring` shows the pool: each account, its limits and when they reset, and which one goes next. When Mainspring moves your session to another account, pi tells you which one and why, like `5-hour limit hit`.

## T3 Code

T3 Code reads limits from hubs that speak CLIProxyAPI's management API, which Mainspring answers at `/v0/management`.

1. In Mainspring, open **Settings → Usage hub**, turn it on and choose **Create management key**. Copy the key; the panel shows it once.
2. In T3 Code, open **Settings → Providers → Usage providers** and choose **Add hub**.
3. Pick **CLI Proxy**, enter Mainspring's address without a path, like `https://mainspring.your-tailnet.ts.net`, and paste the management key.

The accounts then show under **Usage → Limits**. Only Claude and Codex accounts appear, because T3 Code reads their providers' own usage formats.

The management key can read limits, clear an account's cooldown and spend a Codex rate-limit reset credit. It can't send prompts. **Replace key** under **Settings → Usage hub** stops the old one working.

## Your own tools

`GET /v1/pool` returns every account with its status, its limit windows and when each resets, the next account per provider, and your key's live sessions. Every answer from the chat endpoints also says which account served it, in `X-Mainspring-*` headers. See [`/v1/pool`](api.md#get-v1pool) and [response headers](api.md#response-headers) for the fields.

```sh
curl -s https://mainspring.your-tailnet.ts.net/v1/pool -H "Authorization: Bearer $MAINSPRING_KEY"
```

Windows come from the provider and change over time, so read their `label` and `period_ms` rather than expecting a fixed set.
