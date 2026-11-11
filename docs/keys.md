---
title: Keys
summary: Create a key per machine or tool, limit it to providers, models or a daily token cap, and revoke it.
---

Tools authenticate to Mainspring with keys you create under **Keys**. A key starts with `sk-ms-`. The panel shows it once, with ready-made settings for Claude Code and Codex; Mainspring keeps only a hash.

Create one key per machine or tool. The Keys page then shows each one's usage today and its live sessions, and you can revoke one without touching the others.

## Limits

A key can be limited when you create it:

- **Providers**: only these providers, for example only Claude.
- **Models**: a comma-separated list of model IDs, exactly as `/v1/models` lists them.
- **Daily cap**: a number of tokens per day, like `20M` or `500k`. It counts input and output tokens and resets at midnight UTC.

A request outside a key's limits gets `403`. One over the daily cap gets `429` until midnight UTC.

## Sending a key

Send it as `Authorization: Bearer sk-ms-…` or as `x-api-key: sk-ms-…`. Both work on every endpoint.

## Revoking

Revoking a key refuses it from then on. Its live sessions end at their next request. You can't undo it; create a new key instead.

## Management key

Usage dashboards such as T3 Code read limits through a separate management key, created under **Settings → Usage hub**. It reads account limits and can clear a cooldown or spend a Codex rate-limit reset credit. It can't send prompts. See [`/v0/management`](api.md#v0management).
