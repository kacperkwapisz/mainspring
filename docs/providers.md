---
title: Accounts and providers
summary: What each provider needs to sign in, what Mainspring does with the account, and how to sign in again.
---

Add accounts from **Add account** on the Pool page. Each sign-in is stored encrypted, and Mainspring refreshes it before it expires.

| Provider | How you sign in | Notes |
|---|---|---|
| Claude | Sign in with your Claude account in the browser | Each account's limits show live |
| Codex | Sign in with ChatGPT in the browser | Each account's limits show live |
| Grok | Enter a code at x.ai | |
| Cursor | Sign in with Cursor, or paste an API key | Backs up Claude and Codex unless prefixed |
| OpenCode Console | Paste a service account key | Models show as `opencode/<id>` |
| OpenAI-compatible | A base URL and an optional key | OpenRouter, Groq, a local llama.cpp; models show as `<source>/<id>` |

## Claude and Codex sign-in

Both open the provider's own sign-in page in a new tab. When Mainspring runs on the machine you're using, the tab hands the sign-in back by itself. When it runs elsewhere:

- **Claude** shows a code after you approve. Paste it into the panel.
- **Codex** ends on a page that can't load. Copy that page's whole address into the panel.

Signing in again with an account that's already in the pool updates it instead of adding a second copy.

## Names

Accounts are named by their email. Rename one from its detail panel; a name you choose stays.

## Signing in again

When a provider rejects an account's refresh token (a password change, a revoked session), the account shows **Needs sign-in** and stops taking requests. Choose **Sign in again** on its row.

## Out of rotation

Turn **In rotation** off on an account to keep it out of routing without removing it. Its limits still update.

## Removing

Removing an account deletes its stored sign-in. It doesn't sign the account out at the provider.
