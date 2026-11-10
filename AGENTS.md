# Mainspring (formerly fob)

## Design

Paper file: `Mainspring` (`01M72YS3C4QBEB5E0SK99BPHYG`). Page `02 Pool + System` holds the component sheet (`System`) and the reference Pool screen. Page `03 Screens` holds every other screen. Superseded work lives on the `Archive` pages.

Base style: `dense-neutral` (dark values), adapted below. Rules come from the `design-principles` skill.

- **Layout:** 216px left sidebar (wordmark, nav, endpoint, Lock), main area with 32px side padding.
- **Type:** Geist for everything (it's a dev tool), self-hosted in `internal/panel/web/fonts`.
  - Interface: Geist 400/500. 13/20 body, 12/16 meta, 15/20 group titles.
  - Page titles (24/32) and hero numbers (32/40): Geist 600, letter-spacing -0.03em (`--font-display`). 600 is the one third weight, for this role only.
  - Geist Mono only for real data: keys, URLs, model IDs, timestamps.
- **Colour:**
  - Use the file's tokens.
  - One accent, `#3B9BFF` (`--color-accent`), and only for the Claude next-up row.
  - Primary buttons are ink (`--color-ink`).
  - Warning `#FBBF24` is for Needs sign-in only. Danger `#F87171` is for destructive actions.
- **Highlighted row (next up):** accent wash background (`--color-accent-wash`) plus the accent label and bars. No leading-edge stripe.
- **Notices:** plain card with an icon. No coloured side stripe.
- **Radii:** 6 badges, 8 small controls, 10 buttons/inputs, 14 cards, 18 modals.
- **Rows:** 40px data rows, 44px settings rows.
- **Icons:** Lucide (real icons, not redrawn), 16px, 1.2px stroke. Provider marks are the real logos from LobeHub Icons, monochrome. Both are inlined in `app.js`.
- **Confirms:** destructive actions always use a modal, never a popover.
- **Toasts:** Ding (`@yusufceng1z/ding`, plain-DOM build), pinned and vendored like Alpine.
  - Bottom right, one toast at a time, changing in place.
  - `--tst-radius: 14px`, `--tst-bg: var(--color-surface-hover)`, `--tst-fg: var(--color-text-strong)`.
  - Sound off.
  - Used only for one-off actions finishing or failing.
- **Panel sign-in:** WebAuthn only, with no password.
  - Security keys are registered as non-discoverable FIDO2 credentials (`residentKey: discouraged`, `cross-platform`, hint `security-key`), so they don't use a YubiKey passkey slot.
  - Passkeys are the secondary option.
  - Multiple keys are allowed. Setup recommends adding a second (backup) key, and the last key can't be removed.
  - Recovery: run `mainspring auth reset` on the hub.

## Development

- `make test` runs everything; CI also runs `gofmt -l`, `go vet` and `go test -race`.
- `go run ./cmd/mainspring demo` serves the panel with made-up data at http://127.0.0.1:8318 (in memory, proxy sign-in). `-sign-in` exercises the security-key screens.
- The panel is static: `internal/panel/web` (index.html, app.js, app.css, vendored Alpine and Ding, fonts). No build step; `{{VERSION}}` in index.html is replaced with a content hash.
- Routing lives in `internal/route`; the proxy loop in `internal/proxy/proxy.go` calls it. Quota windows are dynamic: never hardcode window ids or counts.
- Releases: tag `vX.Y.Z`; GitHub Actions builds binaries (goreleaser) and the multi-arch image `ghcr.io/kacperkwapisz/mainspring`. The Umbrel store lives in its own repo (`mainspring-umbrel`); bump its `version` and image tag per release.
