---
title: Umbrel
summary: How Mainspring runs on Umbrel, which paths skip Umbrel's login, and how to reach it from other machines.
---

On Umbrel, Mainspring runs behind Umbrel's login: the panel at `/` asks for your Umbrel password, so there's nothing to set up when you first open it.

## Paths that skip Umbrel's login

Tools on your other machines can't send Umbrel's login cookie, so these paths skip it, and Mainspring checks its own credentials on each:

| Path | Needs |
|---|---|
| `/v1`, `/v1/*` | A Mainspring key (`sk-ms-…`) |
| `/mcp` | A Mainspring key |
| `/v0/management/*` | The management key, off until you create one |
| `/health` | Nothing; it only answers `{"ok":true}` |
| `/docs`, `/docs/*`, `/llms.txt`, `/llms-full.txt` | Nothing; these docs |

Other apps installed on the same Umbrel can reach the panel without Umbrel's login, as with any Umbrel app. Turn on **Also require a key** under **Settings → Sign-in** if that matters to you.

## Reaching it from other machines

Your tools need an address they can reach:

- **On your home network**: `http://umbrel.local:8317/v1`.
- **Anywhere, over Tailscale**: give Mainspring its own [tailnet address](tailnet.md), with HTTPS.

## Updates

Update Mainspring from the Umbrel App Store. **Update available** in the sidebar tells you when a new release is out; **What's new** in the panel lists what changed.

## Data

Mainspring keeps its database, its signing secret and its tailnet device in the app's data folder, which Umbrel's backups include when you turn them on.
