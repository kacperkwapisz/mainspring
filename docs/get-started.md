---
title: Get started
summary: Install Mainspring, sign in to the panel, add your first accounts and create a key.
---

Mainspring runs on one machine you own (an Umbrel, a home server, a laptop) and pools the AI subscriptions you already pay for behind one endpoint. Your other machines point their tools at it.

## Install

**Umbrel.** Install Mainspring from the Umbrel App Store. Umbrel's login protects the panel, so there's nothing to set up before you open it.

**Docker.**

```sh
docker run -d --name mainspring -p 8317:8317 -v mainspring-data:/data \
  ghcr.io/kacperkwapisz/mainspring:latest
docker logs mainspring   # prints the one-time setup code
```

**Binary.** Download the archive for Linux or macOS from the [releases page](https://github.com/kacperkwapisz/mainspring/releases), unpack it and run `./mainspring`. It keeps its data in `~/.mainspring` and listens on `127.0.0.1:8317`; set `HOST=0.0.0.0` to reach it from other machines.

## Sign in to the panel

Outside Umbrel the panel signs in with security keys and passkeys, with no password. When Mainspring starts with no key added, it prints a setup code in its log:

```text
mainspring setup code: YFZ8-G9FA — open the panel to add your first security key
```

Open the panel over HTTPS or on `localhost`, enter the code and add a key. Add a second one as a backup. See [Sign-in](sign-in.md) for recovery and for adding keys later.

## Add accounts

Choose **Add account** on the Pool page and pick a provider. Claude and Codex open their own sign-in page; Grok uses a device code; Cursor, OpenCode Console and OpenAI-compatible APIs take a sign-in or a key. [Accounts and providers](providers.md) covers each one.

Add as many accounts of a provider as you have. Mainspring spreads new sessions across them by when each one's limit resets.

## Create a key

Under **Keys**, create one key per machine or tool. A key starts with `sk-ms-`, and the panel shows it once, with ready-made settings for Claude Code and Codex. Then [connect your tools](connect.md).

## Install it on your phone

The panel installs as an app and opens full screen, without the browser around it. Open it at an HTTPS address, like your [tailnet address](tailnet.md), then:

- **iPhone**: tap Share, then **Add to Home Screen**.
- **Android**: open Chrome's menu and choose **Install app**.

Installing needs HTTPS: on a plain `http://` address the phone adds a bookmark instead.
