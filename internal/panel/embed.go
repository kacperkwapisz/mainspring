// Package panel holds the operator panel: one HTML page, its script and styles,
// vendored Alpine.js and Ding, and self-hosted fonts. Nothing loads from a CDN.
package panel

import (
	"embed"
	"io/fs"
)

//go:embed web
var files embed.FS

// Web is the panel's static file tree (index.html, app.js, app.css, fonts/…).
var Web, _ = fs.Sub(files, "web")
