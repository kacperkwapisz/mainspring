# Notices

Mainspring includes or links third-party work under these licenses.

## Adapted code

- [ndraiman/pi-cursor-provider](https://github.com/ndraiman/pi-cursor-provider) by Netanel Draiman (MIT), via Onward's `@onward/cursor-provider`. The Cursor protocol, HTTP/2, model list and text-wrap code was ported from TypeScript to Go under `internal/provider/cursor/`, and its OAuth poll flow informs `internal/oauth/cursor.go`. That project credits [ephraimduncan/opencode-cursor](https://github.com/ephraimduncan/opencode-cursor).
- Routing ideas (cooldowns from rate-limit headers, session affinity keys, the management API shape that usage dashboards read) follow [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) (MIT). No code was copied.

Cursor's agent protocol is unofficial and may change without notice.

## Vendored in the panel (`internal/panel/web`)

- [Alpine.js](https://github.com/alpinejs/alpine), MIT.
- [Ding](https://github.com/YusufCeng1z/ding) 0.0.4 by Yusuf Cengiz, MIT. See `internal/panel/web/ding.LICENSE`.
- Geist and Geist Mono fonts by Vercel, SIL Open Font License 1.1.

## Go modules

- [go-webauthn/webauthn](https://github.com/go-webauthn/webauthn), BSD-3-Clause.
- [coder/websocket](https://github.com/coder/websocket), ISC.
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite), BSD-3-Clause.
- [refraction-networking/utls](https://github.com/refraction-networking/utls), BSD-3-Clause.
- Others as listed in `go.mod`, each under its own license.
