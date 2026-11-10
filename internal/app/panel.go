package app

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/panel"
)

// registerPanel serves the single-page panel. The page itself is public; every
// piece of data it shows comes from /api/*, which the Gate protects.
func registerPanel(mux *httpx.Mux) {
	index, _ := fs.ReadFile(panel.Web, "index.html")
	// Cache-bust by content, so a rebuilt binary never serves yesterday's script.
	sum := sha256.New()
	for _, name := range []string{"app.js", "app.css"} {
		b, _ := fs.ReadFile(panel.Web, name)
		sum.Write(b)
	}
	page := []byte(strings.ReplaceAll(string(index), "{{VERSION}}", hex.EncodeToString(sum.Sum(nil))[:12]))
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-eval' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'self'; base-uri 'none'; form-action 'self'")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(page)
	}
	mux.Handle(http.MethodGet, "/", serveIndex)
	mux.Handle(http.MethodGet, "/assets/{file}", func(w http.ResponseWriter, r *http.Request) {
		serveAsset(w, httpx.Param(r, "file"))
	})
	mux.Handle(http.MethodGet, "/assets/fonts/{file}", func(w http.ResponseWriter, r *http.Request) {
		serveAsset(w, "fonts/"+httpx.Param(r, "file"))
	})
}

func serveAsset(w http.ResponseWriter, name string) {
	name = path.Clean("/" + name)[1:]
	if name == "index.html" || strings.HasPrefix(name, ".") {
		http.NotFound(w, nil)
		return
	}
	data, err := fs.ReadFile(panel.Web, name)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
		return
	}
	ctype := mime.TypeByExtension(path.Ext(name))
	switch path.Ext(name) {
	case ".js":
		ctype = "text/javascript; charset=utf-8"
	case ".woff2":
		ctype = "font/woff2"
	case ".svg":
		ctype = "image/svg+xml"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(name, "fonts/") || name == "alpine.min.js" || name == "ding.js" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
