package app

import (
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	mainspring "github.com/kacperkwapisz/mainspring"
	"github.com/kacperkwapisz/mainspring/internal/docs"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/panel"
)

// The docs are public: they hold no secrets, and agents and teammates should
// be able to read them without signing in.

// docsExample is the address the pages use in examples. Each request sees its
// own address there instead, so commands can be copied as they are.
const docsExample = "https://mainspring.your-tailnet.ts.net"

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(firstValue(r.Header.Get("X-Forwarded-Proto")), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func registerDocs(mux *httpx.Mux) {
	version := assetVersion("app.css", "docs.css")
	set, err := docs.Load(mainspring.Docs)
	if err != nil {
		panic(err) // a page is missing or malformed; the docs test catches this before release
	}
	page := template.Must(template.New("docs").Parse(docsTemplate))
	text := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(body))
	}
	render := func(w http.ResponseWriter, r *http.Request, p *docs.Page) {
		origin := requestOrigin(r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'self'; base-uri 'none'")
		_ = page.Execute(w, map[string]any{
			"Page": p, "Body": template.HTML(strings.ReplaceAll(p.HTML, docsExample, origin)),
			"Groups": docsNav(set, p.Slug), "Version": version,
		})
	}
	mux.Handle(http.MethodGet, "/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/get-started", http.StatusFound)
	})
	mux.Handle(http.MethodGet, "/docs/{page}", func(w http.ResponseWriter, r *http.Request) {
		slug := httpx.Param(r, "page")
		if strings.HasSuffix(slug, ".md") {
			if p, ok := set.Get(strings.TrimSuffix(slug, ".md")); ok {
				text(w, strings.ReplaceAll(p.Source(), docsExample, requestOrigin(r)))
				return
			}
		} else if p, ok := set.Get(slug); ok {
			render(w, r, p)
			return
		}
		http.NotFound(w, r)
	})
	mux.Handle(http.MethodGet, "/llms.txt", func(w http.ResponseWriter, r *http.Request) {
		text(w, set.LLMs(requestOrigin(r)))
	})
	mux.Handle(http.MethodGet, "/llms-full.txt", func(w http.ResponseWriter, r *http.Request) {
		text(w, strings.ReplaceAll(set.Full(), docsExample, requestOrigin(r)))
	})
}

type docsNavGroup struct {
	Name  string
	Pages []docsNavItem
}

type docsNavItem struct {
	Slug, Title string
	Current     bool
}

func docsNav(set *docs.Set, current string) []docsNavGroup {
	var out []docsNavGroup
	for _, g := range docs.Order {
		ng := docsNavGroup{Name: g.Name}
		for _, slug := range g.Slugs {
			p, _ := set.Get(slug)
			ng.Pages = append(ng.Pages, docsNavItem{Slug: slug, Title: p.Title, Current: slug == current})
		}
		out = append(out, ng)
	}
	return out
}

const docsTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<title>{{.Page.Title}} · Mainspring docs</title>
<meta name="description" content="{{.Page.Summary}}">
<link rel="alternate" type="text/markdown" href="/docs/{{.Page.Slug}}.md">
<link rel="icon" href="/assets/mark.svg">
<link rel="stylesheet" href="/assets/app.css?v={{.Version}}">
<link rel="stylesheet" href="/assets/docs.css?v={{.Version}}">
</head>
<body class="docs-body">
<div class="docs-shell">
  <aside class="docs-side">
    <div class="docs-side-top">
      <a class="docs-brand" href="/docs/get-started"><img src="/assets/mark.svg" width="20" height="20" alt=""><span class="strong">Mainspring</span><span class="muted">Docs</span></a>
      <details class="docs-pages" open>
        <summary>Pages</summary>
        <nav aria-label="Docs">
          {{range .Groups}}<div class="docs-group"><div class="docs-group-name">{{.Name}}</div>{{range .Pages}}<a href="/docs/{{.Slug}}"{{if .Current}} aria-current="page"{{end}}>{{.Title}}</a>{{end}}</div>{{end}}
        </nav>
      </details>
    </div>
    <div class="docs-side-foot">
      <a class="docs-panel-link" href="/"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m15 18-6-6 6-6"/></svg>Open the panel</a>
      <a class="docs-llms mono" href="/llms.txt">/llms.txt</a>
    </div>
  </aside>
  <main class="docs-main">
    <article class="docs-article">
      <header class="docs-head">
        <div class="docs-head-text"><h1>{{.Page.Title}}</h1><p class="docs-lead">{{.Page.Summary}}</p></div>
        <button class="btn btn-secondary btn-sm docs-copy-page" type="button" data-md="/docs/{{.Page.Slug}}.md"><span class="copy-ico"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg></span>Copy as Markdown</button>
      </header>
      <div class="docs-prose">{{.Body}}</div>
    </article>
    {{if .Page.Headings}}<aside class="docs-toc" aria-label="On this page">
      <div class="docs-toc-name">On this page</div>
      {{range .Page.Headings}}<a href="#{{.ID}}"{{if .Path}} class="mono docs-toc-path"{{end}}>{{.Text}}</a>{{end}}
      <div class="docs-toc-rule"></div>
      <a class="mono docs-toc-md" href="/docs/{{.Page.Slug}}.md">{{.Page.Slug}}.md</a>
    </aside>{{end}}
  </main>
</div>
<div class="sr-only" aria-live="polite" id="copy-status"></div>
<script>
(() => {
  const CHECK = '<svg class="copy-check" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path pathLength="1" d="M20 6 9 17l-5-5"/></svg>';
  const confirm = (btn, what) => {
    const host = btn.querySelector(".copy-ico");
    if (host && !host.querySelector(".copy-check")) host.insertAdjacentHTML("beforeend", CHECK);
    btn.classList.add("is-copied");
    clearTimeout(btn._t); btn._t = setTimeout(() => btn.classList.remove("is-copied"), 1600);
    document.getElementById("copy-status").textContent = "Copied " + what;
  };
  const copy = async (text, btn, what) => { try { await navigator.clipboard.writeText(text); confirm(btn, what); } catch {} };
  // A copy button on every code block.
  document.querySelectorAll(".docs-prose pre").forEach((pre) => {
    const code = pre.querySelector("code");
    const lang = ((code && code.className.match(/language-(\w+)/)) || [])[1] || "";
    const wrap = document.createElement("div");
    wrap.className = "docs-code";
    const names = { sh: "Shell", bash: "Shell", json: "JSON", jsonc: "JSON", toml: "TOML", text: "Text" };
    wrap.innerHTML = '<div class="docs-code-bar"><span>' + (names[lang] || lang.toUpperCase()) + '</span><button class="btn-icon" type="button" aria-label="Copy"><span class="copy-ico"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg></span></button></div>';
    pre.replaceWith(wrap); wrap.appendChild(pre);
    wrap.querySelector("button").addEventListener("click", (e) => copy((code || pre).innerText.replace(/\n$/, ""), e.currentTarget, "the code"));
  });
  const page = document.querySelector(".docs-copy-page");
  page.addEventListener("click", async () => { const r = await fetch(page.dataset.md); copy(await r.text(), page, "the page"); });
  // On phones the page list starts closed.
  if (matchMedia("(max-width: 759px)").matches) document.querySelector(".docs-pages").removeAttribute("open");
})();
</script>
</body>
</html>`

// assetVersion cache-busts stylesheets by content, like the panel's own page.
func assetVersion(names ...string) string {
	sum := sha256.New()
	for _, name := range names {
		b, _ := fs.ReadFile(panel.Web, name)
		sum.Write(b)
	}
	return hex.EncodeToString(sum.Sum(nil))[:12]
}
