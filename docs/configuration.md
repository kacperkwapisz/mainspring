---
title: Configuration
summary: Environment variables, where Mainspring keeps its data, and the settings in the panel.
---

Mainspring needs no configuration to start. Environment variables change where it listens and keeps its data; everything else is in **Settings**.

## Environment variables

| Variable | Default | What it does |
|---|---|---|
| `HOST` | `127.0.0.1` (Docker: `0.0.0.0`) | Address to listen on |
| `PORT` | `8317` | Port to listen on |
| `MAINSPRING_HOME` | `~/.mainspring` | Data folder for the binary |
| `DATABASE_PATH` | `$MAINSPRING_HOME/mainspring.sqlite` (Docker: `/data/mainspring.sqlite`) | SQLite database. Its folder also holds `jwt_secret` and the tailnet device |
| `JWT_SECRET` | generated next to the database | Encrypts stored sign-ins and signs panel sessions. Keep it with the database: without it, stored sign-ins can't be read |
| `MAINSPRING_AUTH` | `webauthn` | `webauthn`: the panel signs in with security keys and passkeys. `proxy`: a sign-in proxy in front, like Umbrel's, signs you in |
| `MAINSPRING_TRUST_PROXY` | unset | `1` trusts `X-Forwarded-Host` and `X-Forwarded-Proto` from a reverse proxy |
| `PUBLIC_URL` | unset | The HTTPS address the panel suggests when it's opened over plain HTTP |
| `MAINSPRING_INSTALL` | detected | `umbrel`, `docker` or `binary`. Picks the update instructions the panel shows |
| `LOG_LEVEL` | `info` | `debug` also logs successful requests; `error` logs only failures |

## Settings

| Section | Setting | Default |
|---|---|---|
| Tailnet address | Mainspring's own device on your tailnet; see [Tailnet address](tailnet.md) | Off |
| Routing | Keep sessions on their account for this long after their last request | 1 hour |
| Routing | Mark an account's limits stale after this long without reading them | 1 hour |
| Routing | Codex over WebSockets | On |
| Cursor | Prefix models with `cursor/` | Off |
| Cursor | List fast twins as `…-fast` | On |
| Cursor | Fall back between Grok and Cursor | Off |
| Request trace | Log how requests were routed: off, on failure, always | Off |
| Keys | Share pool with keys: account names and limits in response headers and `/v1/pool` | On |
| Usage hub | Share limits with dashboards through a management key | Off |
| Web tools | `web_search` and `web_fetch` at `/mcp` | On |
| Sign-in | Security keys and passkeys; with `proxy` auth, also require a key | |
| About | Check GitHub for new releases | On |
