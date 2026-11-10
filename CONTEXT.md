# Mainspring

A self-hosted proxy that pools AI subscriptions (Claude, Codex, Grok, Cursor, OpenCode Console, OpenAI-compatible APIs) behind one OpenAI- and Anthropic-compatible endpoint, and routes each request to an account by session and quota.

## Language

**Credential** (shown as *account* or *source* in the panel):
An OAuth login for one provider account, an OpenCode Console service key, or an OpenAI-compatible source (base URL + key), stored encrypted in SQLite.
_Avoid_: auth file, token

**Pool**:
All credentials of the providers that report quota windows (Claude, Codex), in routing order.

**Window**:
One quota window a provider reports on its responses: an id, a label, how much is used and when it resets. Providers add and change windows, so nothing assumes a fixed set.

**Router** (`internal/route`):
Picks a credential per request: the one the session is pinned to, else the one whose longest window resets soonest with reserve left. Keeps cooldowns, benching and needs-sign-in state.
_Avoid_: scheduler, balancer

**Session**:
One coding session, identified from what the client sends (Claude Code session id, Codex thread id, prompt cache key, else the LocalKey). Pinned to one credential for the affinity window.

**Cooldown** (*resting* in copy):
A credential skipped until a time, after a 429 or repeated auth failures. Spent windows cool until they reset.

**Bench** (*in rotation* off):
A credential the operator took out of rotation.

**LocalKey**:
A `sk-ms-…` secret handed to a tool. Stored hashed; sent as Bearer or `x-api-key`.
_Avoid_: API key, user

**Management key**:
A read-only `ms-hub-…` secret usage dashboards use on `/v0/management`.

**Executor**:
The module that speaks one provider's wire format.
_Avoid_: client, adapter, driver

**Vault**:
The SQLite store of credentials, encrypted with `JWT_SECRET`.

**Meter**:
Append-only usage events with API-equivalent dollar estimates at models.dev list prices. Not a bill.

**Gate**:
Decides who may use the panel: a security-key session, or a trusted sign-in proxy in front.
