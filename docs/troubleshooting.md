---
title: Troubleshooting
summary: What common errors mean and what to do about them.
---

Every error Mainspring returns says what happened and what to change. These are the common ones.

## `401` invalid api key

The tool didn't send a Mainspring key, or sent one that was revoked. Send it as `Authorization: Bearer sk-ms-…` or `x-api-key`. Claude Code reads it from `ANTHROPIC_AUTH_TOKEN`, not `ANTHROPIC_API_KEY`.

## `404` unknown model

The model ID isn't one Mainspring can serve. Check the spelling against `GET /v1/models`. If the model is listed in the panel but dimmed, it's [turned off](models.md).

## `403` key is not scoped

The key is limited to other providers or models. See [Keys](keys.md).

## `429` every account is resting

Every account of that provider hit a limit. `Retry-After` says when the first one is back. Add another account, or use a model from another provider until then.

## `429` daily token cap reached

The key's daily cap is used up. It resets at midnight UTC, or raise the cap on the Keys page.

## An account says Needs sign-in

The provider rejected its refresh token, usually after a password change or a sign-out everywhere. Choose **Sign in again** on its row; see [Accounts and providers](providers.md#signing-in-again).

## Limits show as stale

Mainspring hasn't read the account's limits for a while. Idle accounts are read every 30 minutes from the provider's usage endpoint. To read them straight away, choose **Check** on the account's row or **Check now** in its detail panel.

## Check says the provider is limiting how often limits can be read

The provider's usage endpoint answered 429. That's about reading limits, not the account's own quota. Mainspring tries again in 30 minutes, and the account's limits also update with every request it serves.

## Security keys aren't offered

Browsers only offer security keys and passkeys over HTTPS or on `localhost`, never on a plain `http://` address or an IP. Open the panel at its HTTPS or [tailnet address](tailnet.md).

## Codex turns stall

Something between Codex and Mainspring may be dropping WebSocket connections. Set `supports_websockets = false` in Codex's provider settings, or turn off **Codex over WebSockets** under **Settings → Routing**.

## Seeing what happened to a request

Turn on **Settings → Request trace** (on failure, or always). Mainspring then logs how each request was routed and what came back. Traces never contain tokens or keys.
