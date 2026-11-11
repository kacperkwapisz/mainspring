// Package mainspring holds files that ship inside the binary from the repo root.
package mainspring

import "embed"

// Changelog is CHANGELOG.md, shown under What's new in the panel.
//
//go:embed CHANGELOG.md
var Changelog string

// Docs is the docs/ folder: one Markdown page per file, shown at /docs and
// served raw at /docs/<page>.md and /llms.txt for agents.
//
//go:embed docs/*.md
var Docs embed.FS
