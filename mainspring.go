// Package mainspring holds files that ship inside the binary from the repo root.
package mainspring

import _ "embed"

// Changelog is CHANGELOG.md, shown under What's new in the panel.
//
//go:embed CHANGELOG.md
var Changelog string
