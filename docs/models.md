---
title: Models
summary: Where the model list comes from, which IDs to use, and how to turn a model off.
---

`GET /v1/models` lists every model your accounts can serve, and **Sources → Models** in the panel shows the same list with context sizes and effort levels.

## Where the list comes from

- **Claude and Codex**: each signed-in account's own model list, from the provider, so you see exactly what your subscriptions offer, with each model's real context size and effort levels. Lists are kept for an hour and refreshed in the background.
- **Cursor, Grok and the rest**: the provider's catalogue. Prices for the meter come from [models.dev](https://models.dev), refreshed daily.

New models appear on their own, without a Mainspring update.

## Prices

The meter and the `cost` in `/v1/models` are API list prices per million tokens. Some models cost more once a request's input (uncached, cache reads and cache writes together) passes a size, such as 100k tokens for Claude Haiku 5.5 or 272k for GPT-5.5. `cost.tiers` lists those prices, and the meter prices a request that passes the size at the higher rates:

```json
"cost": {"input": 0.1, "output": 0.5, "cache_read": 0.01, "cache_write": 0.125,
  "tiers": [{"input_tokens_above": 100000, "input": 0.5, "output": 2.5, "cache_read": 0.05, "cache_write": 0.625}]}
```

Models you add from an OpenAI-compatible source get a price and size limits from models.dev when their ID (or the part after the last `/`) is one it lists, and the meter charges that price. Other models have no price.

Cursor IDs that the vendors spell differently, such as `claude-4.5-sonnet` or `gemini-3.1-pro`, take the vendor's price and limits for that model.

## IDs

Use IDs exactly as `/v1/models` lists them. Variants that tools add to a listed ID, like a `[1m]` context tag or a release date, count as that ID.

## Turning a model off

Turn a model off with its switch under **Sources → Models**. It leaves `/v1/models`, so tools stop offering it, and requests for it get `404` with a message saying it's turned off. Cursor doesn't step in as a fallback for it.

`cursor/claude-…` IDs are separate entries; turn those off too if you want Cursor's copy gone.
