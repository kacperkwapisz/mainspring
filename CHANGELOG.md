# Changelog

Mainspring shows the newest entries under **What's new** in the panel, and each GitHub release uses its version's entry as its notes. Write for the people running Mainspring: what changed for them and why.

## [1.0.0-rc.13] - 2026-11-11

- Fixes Claude requests with a system prompt from tools other than Claude Code, which failed with "role 'system' is not supported" since 1.0.0-rc.4.
- Older Claude models such as Haiku 4.5 now accept a system prompt too.
- Fixes empty answers from Codex models when a tool doesn't stream the response.

## [1.0.0-rc.12] - 2026-11-11

- **What's new** in the panel shows what changed in each version.
- Mainspring checks GitHub for new releases and tells you in the sidebar when one is out, with how to update for your install. Turn it off in Settings.

## [1.0.0-rc.11] - 2026-11-11

- The model list shows exactly what your Claude and Codex subscriptions offer, each model with its real context size and effort levels. Models your accounts can't use no longer show up.
- New models appear on their own, without a Mainspring update.

## [1.0.0-rc.10] - 2026-11-11

- Adds Claude Fable 5.1 and 5.2, Haiku 5.5, Sonnet 5.5 and Opus 5.5.
- Every Fable version counts against the weekly Fable limit.

## [1.0.0-rc.9] - 2026-11-10

- Turn any model off under **Sources → Models**. It leaves the model list your tools see, and requests for it are refused.

## [1.0.0-rc.8] - 2026-11-10

- The panel updates the moment a limit resets or a cooldown ends, then reads the account's new limits.

## [1.0.0-rc.7] - 2026-11-10

- The panel updates live: limits, sessions, usage and accounts change on screen as they happen, without reloading.

## [1.0.0-rc.6] - 2026-11-10

- Accounts are named by their email. Signing in a second Claude account no longer replaces the first.
- New and idle accounts get their limits from the provider's usage endpoint, so they don't show as stale.

## [1.0.0-rc.5] - 2026-11-10

- Requests carry the current Claude Code and Codex client details.
- Icons sit centred in their buttons.

## [1.0.0-rc.3] - 2026-11-10

- Claude sign-in starts exactly as Claude Code does, which fixes "Authorization failed, Invalid request format".
