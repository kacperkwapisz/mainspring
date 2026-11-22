# Changelog

Mainspring shows the newest entries under **What's new** in the panel, and each GitHub release uses its version's entry as its notes. Write for the people running Mainspring: what changed for them and why.

## [1.9.0] - 2026-11-22

- Generate and edit images on your ChatGPT plans. Mainspring serves the OpenAI Images API at `/v1/images/generations` and `/v1/images/edits` on your Codex accounts, the same way Codex's own image tool uses the plan. Use `gpt-image-2` as the model; ChatGPT serves GPT Image 2 on Codex accounts whatever model you name. Image quota is separate from chat: an account that runs out of image quota passes the call to your next Codex account and keeps serving chat, and an account resting for a chat limit can still make images.
- pi gets `gpt-image-2` as a Mainspring image model when your pool has a Codex account, so codemode scripts can call `models.generateImages()` on your plan. Run `pi update` to get it.

## [1.8.3] - 2026-11-17

- Fixes Claude Code requests failing with "A maximum of 4 blocks with cache_control may be provided. Found 5." Mainspring added a cache breakpoint of its own to Anthropic-format requests whose tools had none. Claude Code already uses all four, so it now gets its request through as sent. 1.8.2 had stopped trimming Claude Code's breakpoints, which used to hide this.

## [1.8.2] - 2026-11-17

- Mainspring forwards requests as your client sent them, and changes them only where the provider needs it: translating between API formats, and the Claude Code identity subscription accounts use. When a provider refuses something, you now get its error instead of a quiet change:
  - `temperature`, `top_p` and `top_k` reach Claude as sent.
  - Thinking stays on when a request forces a tool.
  - Empty web-search domain lists are passed on.
  - `developer` messages reach OpenAI-compatible sources as sent.
  - Mainspring no longer adds `stream_options.include_usage` to requests in OpenAI chat format, so their streamed usage is recorded only when the client asks for it.
  - Cache breakpoints are trimmed only to make room for the ones Mainspring adds itself, and never on Claude Code's own requests.
  - Claude Code's own mid-conversation system messages stay where it put them, so its prompt cache isn't disturbed.
- The one-hour Claude prompt cache Mainspring adds when a tool sets no cache time is now a setting, **Keep Claude's prompt cache for an hour** under Settings → Routing. It's on by default, as before.
- Token counting on Codex, Grok and Cursor models returns an error saying only Claude models can count tokens, instead of an estimate.
- Claude web search no longer drops sites it can't reach, such as reddit.com, and no longer moves such a search to another provider. Doing so changed what was searched without saying so. A search that names one of these sites fails again with Anthropic's message, which names the site.

## [1.8.1] - 2026-11-17

- Fixes Claude refusing requests from clients that set the effort per turn or bind thinking blocks, such as pi's built-in Claude 5.5 models, with "output_config: Extra inputs are not permitted" or a thinking error. Mainspring now passes on the betas these clients ask for and keeps their per-turn messages.
- Web search on Claude no longer fails when a request names a site Claude's search can't reach, such as reddit.com. Mainspring drops that site and searches again, and tries another provider when it was the only allowed site.
- A client that hangs up while sending its request is logged as cancelled rather than as a malformed request, and an oversized request gets a 413.

## [1.8.0] - 2026-11-16

- pi shows what each session would cost at API list prices, including long-context pricing. Your subscriptions pay for the tokens; the figure is the value you get. Run `pi update` to get it.
- The Meter's dollar figures are right for every provider:
  - Codex, Grok, Cursor and OpenAI-compatible sources no longer count cached tokens twice. Their old Meter rows are corrected once when you update.
  - Claude's one-hour cache writes cost twice the input price, as Anthropic charges them, and web searches add $0.01 each.
  - Claude used from an OpenAI-format client, such as Codex CLI, counts its cache reads and writes again. Before, they were dropped.
  - OpenAI-compatible sources report token usage on streamed requests, and Codex requests that stop early keep their tokens.
  - Long-context pricing applies above each model's threshold. Cursor's own names for Claude, Gemini and GLM models, Cursor's fast models and models from OpenAI-compatible sources now get prices.
- Token counts sent back to clients follow each API's rules. OpenAI-format clients get input that includes the cache, and Anthropic-format clients get input without it, so Codex CLI and Claude Code see how full their context really is.
- A key's daily token cap now leaves cached input out for every provider, as it already did for Claude.
- Token counting (`/v1/messages/count_tokens`) on Codex, Grok and Cursor models answers with an estimate instead of running the request, and token counts no longer show up on the Meter.
- Choosing a thinking effort in an OpenAI-format client sets Claude's effort on models that support it, rather than a thinking budget. pi does the same for Mainspring's Claude models.
- System prompts sent as `developer` messages reach Cursor, Grok, Claude and OpenAI-compatible sources as system prompts. Before, Cursor ignored them and Claude got them as user messages.
- pi's web tools run on the account your session is using, not on another session's.
- `GET /v1/models` lists each model's long-context prices (`cost.tiers`) and the API that suits it best (`api`).
- pi starts without waiting for Mainspring, keeps a separate model list for each Mainspring it connects to, and says which account a session moved from and to. Leaving the address empty in `/mainspring setup` keeps the saved one.

## [1.7.2] - 2026-11-14

- Fixes Claude accounts asking you to sign in again after a few days. When several requests reached an account just as its token expired, each one refreshed it with the same refresh token. Claude accepts a refresh token once, so all but the first were refused and the account was marked **Needs sign-in**. Mainspring now refreshes each account once, and the other requests use the new token.
- The usage endpoint for dashboards (`/v0/management/api-call`) now saves a token it refreshes. Before, it threw the new token away, so the account's next refresh failed.
- Tokens are refreshed in the background up to an hour before they expire, so refreshes rarely happen while requests are waiting.

## [1.7.1] - 2026-11-13

- Mainspring also follows OpenAI's and Cursor's status pages, so Codex and Cursor incidents show on the Pool and Log pages and in failed requests' messages, like Claude's.

## [1.7.0] - 2026-11-13

- Mainspring follows Anthropic's status page. During an incident that affects the Claude API or Claude Code, the Pool and Log pages say so with a link, a failed request's message names the incident, and the server log notes when it opens and closes.
- pi's `/mainspring` shows open incidents too, closes on Esc in every terminal, sits in a frame and groups live sessions by account. Run `pi update` to get it.

## [1.6.1] - 2026-11-12

- Fixes Claude refusing some requests with "A maximum of 4 blocks with cache_control may be provided". Requests Mainspring reshapes for Claude now keep at most four cache breakpoints, keeping the ones at the start and at the end of the conversation.

## [1.6.0] - 2026-11-12

- Claude sessions keep their prompt cache for an hour instead of five minutes, as long as they stay on their account. A pause over five minutes no longer writes the whole prompt again, which used up limits faster. One-off requests keep five minutes, and a tool that sets its own cache TTL keeps it.
- The pi extension offers every thinking level a model accepts, including xhigh and max, and hides levels it doesn't. Run `pi update` to get it.

## [1.5.0] - 2026-11-12

- New Log page. Failed requests lists every request that failed in the last 14 days, with the message its app got and the account, model and key it used. Server log shows what Mainspring wrote, live, with Copy and Download.
- The sidebar shows how many requests failed today.
- Request traces now show in the panel, under Log → Server log.

## [1.4.1] - 2026-11-12

- The Meter chart's Today label sits centred under its bar, like the other days.

## [1.4.0] - 2026-11-12

- New setting, Settings → Timezone: where days start for Today in the panel, the Meter's days and each key's daily cap. The first time you open the panel it's set to your browser's timezone.
- A key's daily cap now resets at midnight in that timezone instead of midnight UTC.

## [1.3.3] - 2026-11-12

- Today, and the days on the Meter chart, follow your own clock. They used the server's, which on Umbrel and Docker is UTC, so just after midnight Today still showed yesterday.

## [1.3.2] - 2026-11-11

- Fixes Codex over WebSockets at the connection's hourly reconnect: a turn that was still running could be cut off before its last message, so Codex saw it as unfinished.

## [1.3.1] - 2026-11-11

- New docs page, Your usage in other tools: see your limits in pi, T3 Code or your own scripts, with the steps to connect T3 Code.
- The docs are shorter: pages no longer repeat each other, and an outdated troubleshooting entry is gone.

## [1.3.0] - 2026-11-11

- What's new shows the version you have installed and the latest release, and whether you're up to date.
- Text keeps its sizes on iPhones and the iPhone Duo: iOS no longer enlarges some blocks on wide or landscape screens, which broke the hierarchy.
- Sessions stack into readable rows on screens narrower than 1100px, like the Duo unfolded.
- The installed app opens like Umbrel's: standalone, without the browser around it.

## [1.2.1] - 2026-11-11

- Fixes an empty Sessions list when several pi sessions shared an account: their short IDs came out the same.
- When Anthropic or OpenAI limits how often limits can be read, Mainspring waits 30 minutes before reading that account again and says so, instead of "claude sub 429". Idle accounts are read every 30 minutes instead of 15.

## [1.2.0] - 2026-11-11

- Install Mainspring on your phone: the panel is an app that opens full screen, from Add to Home Screen on iPhone or Install app on Android. It needs an HTTPS address, like your tailnet address.
- The Meter has a **Web tools** card: how many searches and page questions your apps ran, by provider.
- The Tokens per day chart is cleaner: readable labels at any width, a scale, and the day's total on hover.
- Fixes the Meter for Claude Code, pi and other Anthropic-format tools: their streamed answers were counted as failed with no tokens, so cost and the cache share read zero.

## [1.1.0] - 2026-11-11

- **Mainspring for pi**: a pi extension in this repo (`pi install git:github.com/kacperkwapisz/mainspring`). It adds Mainspring as a provider, registers `web_search` and `web_fetch` on your subscriptions, tells you when your session moves to another account, and shows the pool with `/mainspring`.
- Answers say which account served them and its limits, in `X-Mainspring-*` headers, and `GET /v1/pool` shows the pool to tools holding a key. Turn both off with **Share pool with keys** under **Settings → Keys**.

## [1.0.4] - 2026-11-11

- Docs inside Mainspring at `/docs`: getting started, connecting Claude Code, Codex, Cursor, OpenCode and pi, every guide and the full API reference. Examples show your own address, so commands can be copied as they are.
- For agents, each page is Markdown at `/docs/<page>.md`, and `/llms.txt` lists them all.
- **Docs** in the sidebar opens them.
- The management key's description says what it can do: read limits, clear a cooldown and spend a Codex reset credit. It never sends prompts.

## [1.0.3] - 2026-11-11

- Copying confirms on the button itself: its icon turns into a check for a moment, instead of a toast.
- Toasts appear at the top centre.

## [1.0.2] - 2026-11-11

- Settings → Tailnet address has an **Open it** button once Mainspring is connected, so you can always get to the tailnet address, with a setup code while no security key exists for it.
- The setup screen says where to find the setup code on Umbrel, instead of pointing at a terminal.

## [1.0.1] - 2026-11-11

- Scrollbars are thin, with no track behind them.

## [1.0.0] - 2026-11-11

The first stable release. Everything from the release candidates below, in one place:

- Pools Claude, Codex, Grok, Cursor, OpenCode Console and OpenAI-compatible accounts behind one OpenAI- and Anthropic-compatible endpoint.
- Sends new sessions to the account whose limit resets soonest, keeps each session on its account so prompt caches stay warm, and rests an account that runs out until its limit resets.
- Shows every account's limits live, read from responses and from each provider's usage endpoint, with the models each subscription offers.
- Web search and fetch for your apps over MCP, on the account your session uses.
- Its own HTTPS address on your Tailscale network, set up from Settings.
- A panel that updates live, signs in with security keys or passkeys, and tells you when a new release is out.

## [1.0.0-rc.15] - 2026-11-11

- Mainspring can join your Tailscale network as its own device, with an HTTPS address every device on your tailnet can reach. Add it under **Settings → Tailnet address**: name it, approve it in Tailscale, done.
- The panel on the tailnet address always asks for a security key, because that address skips Umbrel's login. The first time, **Open it** brings a one-time setup code to add one.
- Dialogs rise from the bottom of the screen on phones.

## [1.0.0-rc.14] - 2026-11-11

- Mainspring is an MCP server at `/mcp` with `web_search` and `web_fetch`. Each call runs on the Claude or Codex account your app's session already uses, and shows on the meter. Setup lines for Claude Code, Codex and pi are under **Settings → Web tools**.
- `web_fetch` never reaches addresses on your local network.

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
