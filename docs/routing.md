---
title: How routing works
summary: How Mainspring picks a provider and an account for each request, keeps sessions on one account, rests accounts that run out, and falls back.
---

## Provider

The model ID picks the provider:

| Model ID | Goes to |
|---|---|
| `claude-…` | Claude, then Cursor on retryable errors |
| `gpt-…`, `o1…`, `o3…`, `o4…` | Codex, then Cursor on retryable errors |
| `grok-…` | Grok (and Cursor's Grok, when Grok fallback is on) |
| `composer-…`, `gemini-…`, `kimi-…`, `…-fast`, `cursor/…` | Cursor |
| `opencode/…` | OpenCode Console |
| `<source>/…` | That OpenAI-compatible source |

With **Prefix models with cursor/** on (**Settings → Cursor**), Cursor answers only to `cursor/…` IDs and stops backing up Claude and Codex. A model you [turn off](models.md) is refused, fallback included.

## Account

Within a provider, Mainspring picks an account in this order:

1. **The account the session already uses**, while the session has sent a request within the last hour. Staying on one account keeps the provider's prompt cache warm, which makes long sessions faster and cheaper on your limits. Change the hour under **Settings → Routing**.
2. **Otherwise the account whose longest limit resets soonest** and still has reserve left, so quota you've paid for gets used before it resets.
3. **Accounts whose limits Mainspring hasn't read yet**, oldest first.

Accounts that are out of rotation, waiting for you to sign in again, or resting after a limit are skipped.

### Sessions

Mainspring recognises a session from what the tool sends: Claude Code's session ID, Codex's thread ID, a `session_id`, `prompt_cache_key` or `conversation_id` in the body, or an `X-Session-ID` header. Requests without any of these are routed one by one.

## Limits

Claude and Codex report each account's limits (a 5-hour window, a weekly one, sometimes per-model ones) with every response. Mainspring reads them as they come, and for accounts that haven't served a request in 15 minutes it asks the provider's usage endpoint instead. Providers add and rename windows over time; Mainspring follows whatever they report.

When an account hits a limit, the provider answers `429` with the time the window resets. Mainspring rests the account until then, moves the session to another account and records it in the activity log. When the window resets, the account comes back on its own.

If every account of a provider is resting, clients get `429` with `Retry-After` set to when the first one is back.

## Fallback

On a retryable error before any output has streamed, Mainspring tries the next account, then the fallback provider. Once a response has started streaming, it can't switch without the tool seeing a broken answer, so the error goes to the tool.
