---
title: Models
summary: Where the model list comes from, which IDs to use, and how to turn a model off.
---

`GET /v1/models` lists every model your accounts can serve, and **Sources → Models** in the panel shows the same list with context sizes and effort levels.

## Where the list comes from

- **Claude and Codex**: each signed-in account's own model list, from the provider, so you see exactly what your subscriptions offer, with each model's real context size and effort levels. Lists are kept for an hour and refreshed in the background.
- **Cursor, Grok and the rest**: the provider's catalogue. Prices for the meter come from [models.dev](https://models.dev), refreshed daily.

New models appear on their own, without a Mainspring update.

## IDs

Use IDs exactly as `/v1/models` lists them. Variants that tools add to a listed ID, like a `[1m]` context tag or a release date, count as that ID.

## Turning a model off

Turn a model off with its switch under **Sources → Models**. It leaves `/v1/models`, so tools stop offering it, and requests for it get `404` with a message saying it's turned off. Cursor doesn't step in as a fallback for it.

`cursor/claude-…` IDs are separate entries; turn those off too if you want Cursor's copy gone.
