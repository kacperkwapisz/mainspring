# Mainspring for pi

A [pi](https://pi.dev) extension for [Mainspring](https://github.com/kacperkwapisz/mainspring).

- **Provider.** Adds Mainspring as the `mainspring` provider, with the models your subscriptions offer. Claude models speak the Anthropic API and GPT models the Responses API, so prompt caching and sessions behave as they do directly. Each pi session stays on one Mainspring account.
- **Web tools.** Registers `web_search` and `web_fetch`. They run on the Claude or Codex account your session already uses. They replace pi-claude-web, which registers the same names; remove it first.
- **Account moves.** When Mainspring moves your session to another account, pi tells you which and why.
- **`/mainspring`.** Shows the pool: every account, its limits as bars with how far into the window you are, when each resets, which goes next, and your live sessions. `r` refreshes, `esc` closes.

## Install

```sh
pi install git:github.com/kacperkwapisz/mainspring
```

Run `/mainspring setup` and enter Mainspring's address and a key from its **Keys** page, or set `MAINSPRING_URL` and `MAINSPRING_KEY`. Settings are saved in `~/.pi/agent/mainspring.json`. Then pick a model under **Mainspring** with `/model`.

Account names, limits and `/mainspring` need **Share pool with keys** on in Mainspring (**Settings → Keys**, on by default).
