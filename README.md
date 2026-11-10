# Fob

A local proxy that turns Claude, Codex, Grok, and Cursor subscriptions — plus OpenCode Console inference and any OpenAI-compatible API — into one OpenAI-compatible endpoint. One panel for logins, sources, the keys you hand to other tools, and the usage meter.

Point Cursor, Claude Code, OpenCode, or anything that speaks OpenAI/Anthropic at `http://127.0.0.1:8317/v1`.

## Run (binary)

Download a release for your OS from GitHub Releases, then:

```bash
chmod +x fob
./fob
```

On first boot without `JWT_SECRET`, Fob writes `~/.mainspring/jwt_secret` (`0600`) and `~/.mainspring/mainspring.sqlite`. While no security key is added, every boot prints a one-time setup code (`mainspring setup code: XXXX-XXXX`). Open http://localhost:8317, enter the code, add a security key (a passkey works too), login to a provider, mint a `sk-ms-…` key.

Panel sign-in uses WebAuthn, so it needs HTTPS or `localhost`. Lost every key? Run `mainspring auth reset` on the hub: it removes all keys, signs out every panel session and prints a new setup code. `mainspring auth status` lists the mode and keys.

## Run (Docker)

```bash
export JWT_SECRET=$(openssl rand -hex 32)
docker compose up --build
```

Compose **requires** `JWT_SECRET`. Data lives on the `mainspring-data` volume at `/data/mainspring.sqlite`.

## OAuth

Claude and Codex OAuth apps only allow their CLI callbacks (`http://localhost:54545/callback` and `http://localhost:1455/auth/callback`). While a login is in flight, Fob binds those ports. If the port is already taken (the real CLI), the panel keeps the paste-callback form — paste the whole failed address bar. Grok uses device-code. Cursor uses CLI deep-control poll (Login) or a dashboard API key (Paste key). OpenCode uses a Console service account key; models list as `opencode/<id>` and route to chat, responses, or Anthropic messages by model family (no Gemini/Jev). OpenAI-compatible sources are unlimited: paste a base URL and optional key (OpenRouter, Groq, a local llama.cpp, anything that speaks `/v1`). Local servers can omit the key. Source models list as `slug/id` so they never collide with a sub.

## Env

| Var | Default | Role |
|---|---|---|
| `JWT_SECRET` | binary: `~/.mainspring/jwt_secret` (auto); Docker: required | Encrypts credentials and signs the panel cookie |
| `MAINSPRING_HOME` | `~/.mainspring` | Binary data dir (`mainspring.sqlite`, `jwt_secret`) |
| `DATABASE_PATH` | binary: `$MAINSPRING_HOME/mainspring.sqlite`; image: `/data/mainspring.sqlite` | SQLite |
| `MAINSPRING_AUTH` | `webauthn` | `webauthn`: panel sign-in with a security key or passkey. `proxy`: the reverse proxy in front (e.g. Umbrel) signs the operator in; the panel can still require a key |
| `MAINSPRING_TRUST_PROXY` | unset | `1` honours `X-Forwarded-Host` / `X-Forwarded-Proto` for the sign-in origin (always on in `proxy` mode) |
| `HOST` | `0.0.0.0` | Bind |
| `PORT` | `8317` | Listen |
| `LOG_LEVEL` | `info` | Failures always log one line to stderr. `debug` also logs successful hops. Optional panel **Request trace** (`off` / `errors` / `always`) dumps hop breadcrumbs on failure or always. No tokens. |
| `CLAUDE_CLIENT_ID` / `CODEX_CLIENT_ID` / `GROK_CLIENT_ID` | embedded CLI clients | Override |

Cursor effort variants (`-high`, `-medium`, …) collapse to one listed id (`claude-opus-5`, `composer-2.5`). Thinking stays a sibling (`claude-opus-5-thinking`). Fast twins list as `…-fast` by default (`composer-2.5-fast`); turn that off in the panel Cursor card. Pick effort with `reasoning_effort` and fast with `fast: true` or a `-fast` suffix. Ids that already appear on a connected Claude/Codex/Grok catalog are omitted; force Cursor with a `cursor/` prefix (`cursor/claude-opus-5`). Unprefixed `claude-opus-5` still hits Anthropic first and failovers to Cursor on retryable errors. Optional panel toggle maps Grok ↔ Cursor Grok.

## API

- `GET /health`
- `GET /v1/models` — OpenAI list plus discovery fields: `name`, `context_length`, `max_output_tokens`, `input` / `input_modalities`, `reasoning`, `efforts`, `cost`. Twin ids (`*-thinking`, `*-reasoning`, `*-fast`) stay siblings; a ladder is only advertised when that id actually takes `reasoning_effort`.
- `POST /v1/chat/completions`
- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `POST /v1/responses`

Bearer: a LocalKey (`sk-ms-…`). Meter dollars are **API-equivalent $** from [models.dev](https://models.dev) list prices, not your subscription bill. The panel Sub card is remaining on the subscription (click to load).

## Dev

```bash
make test
go run ./cmd/mainspring
```

Live HTTP against a real vault (skips unless you ask):

```bash
make live                         # boots ~/.mainspring, mints a throwaway key
MAINSPRING_LIVE_URL=https://hub.example.com MAINSPRING_LIVE_KEY=sk-ms-… make live
```
