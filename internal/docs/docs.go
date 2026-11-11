// Package docs reads the Markdown pages in docs/ and renders them for the
// panel, for agents (/docs/<page>.md) and for /llms.txt.
package docs

import (
	"bytes"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Order is the sidebar: groups and their pages, by slug.
var Order = []Group{
	{"Start", []string{"get-started", "connect"}},
	{"Guides", []string{"routing", "providers", "keys", "usage", "models", "web-tools", "tailnet", "sign-in", "umbrel", "troubleshooting"}},
	{"Reference", []string{"api", "configuration"}},
}

// Group is one sidebar section.
type Group struct {
	Name  string
	Slugs []string
}

// Page is one doc page.
type Page struct {
	Slug     string
	Title    string
	Summary  string
	Group    string
	Markdown string // the body, without front matter
	HTML     string // rendered body
	Headings []Heading
}

// Heading is an h2 for the "On this page" list. Endpoint headings
// ("POST /v1/messages") list their path in monospace.
type Heading struct {
	ID, Text string
	Path     bool
}

// Set is every page, in sidebar order.
type Set struct {
	Pages  []*Page
	bySlug map[string]*Page
}

// Get returns the page for slug.
func (s *Set) Get(slug string) (*Page, bool) {
	p, ok := s.bySlug[slug]
	return p, ok
}

var (
	endpointHeading = regexp.MustCompile(`^(?:(GET|POST|PUT|DELETE) )?(/\S*)$`)
	endpointHTML    = regexp.MustCompile(`<h2 id="([^"]+)">((GET|POST|PUT|DELETE) )?(/[^<\s]*)</h2>`)
)

var frontMatter = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n`)

// Load reads docs/*.md from fsys and renders every page. Every page in Order
// must exist, and every file must be in Order, so nothing is left out of the
// sidebar or /llms.txt.
func Load(fsys fs.FS) (*Set, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithASTTransformers(util.Prioritized(linkRewriter{}, 100))),
	)
	set := &Set{bySlug: map[string]*Page{}}
	listed := map[string]bool{}
	for _, g := range Order {
		for _, slug := range g.Slugs {
			listed[slug] = true
			raw, err := fs.ReadFile(fsys, "docs/"+slug+".md")
			if err != nil {
				return nil, fmt.Errorf("docs: %s is in the sidebar but docs/%s.md is missing", slug, slug)
			}
			p, err := parse(md, slug, g.Name, string(raw))
			if err != nil {
				return nil, err
			}
			set.Pages = append(set.Pages, p)
			set.bySlug[slug] = p
		}
	}
	files, _ := fs.Glob(fsys, "docs/*.md")
	for _, f := range files {
		slug := strings.TrimSuffix(strings.TrimPrefix(f, "docs/"), ".md")
		if !listed[slug] {
			return nil, fmt.Errorf("docs: docs/%s.md isn't in the sidebar order", slug)
		}
	}
	return set, nil
}

func parse(md goldmark.Markdown, slug, group, raw string) (*Page, error) {
	m := frontMatter.FindStringSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("docs: docs/%s.md needs front matter with a title and a summary", slug)
	}
	p := &Page{Slug: slug, Group: group, Markdown: strings.TrimSpace(raw[len(m[0]):])}
	for _, line := range strings.Split(m[1], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "title":
			p.Title = strings.TrimSpace(v)
		case "summary":
			p.Summary = strings.TrimSpace(v)
		}
	}
	if p.Title == "" || p.Summary == "" {
		return nil, fmt.Errorf("docs: docs/%s.md needs a title and a summary", slug)
	}
	src := []byte(p.Markdown)
	doc := md.Parser().Parse(text.NewReader(src))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering && h.Level == 2 {
			id, _ := h.AttributeString("id")
			idStr := ""
			if b, ok := id.([]byte); ok {
				idStr = string(b)
			}
			hd := Heading{ID: idStr, Text: string(h.Text(src))}
			if m := endpointHeading.FindStringSubmatch(hd.Text); m != nil {
				hd.Text, hd.Path = m[2], true
			}
			p.Headings = append(p.Headings, hd)
		}
		return ast.WalkContinue, nil
	})
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return nil, err
	}
	// Endpoint headings get a method badge and a code path.
	p.HTML = endpointHTML.ReplaceAllStringFunc(buf.String(), func(h string) string {
		m := endpointHTML.FindStringSubmatch(h)
		method := ""
		if m[3] != "" {
			method = `<span class="docs-method">` + m[3] + `</span>`
		}
		return `<h2 id="` + m[1] + `" class="docs-endpoint">` + method + `<code>` + m[4] + `</code></h2>`
	})
	return p, nil
}

// linkRewriter turns links between pages ("keys.md#limits") into panel links
// ("/docs/keys#limits"). The Markdown keeps the .md form, which works on
// GitHub and for agents reading /docs/<page>.md.
type linkRewriter struct{}

var pageLink = regexp.MustCompile(`^([a-z0-9-]+)\.md(#[a-z0-9-]+)?$`)

func (linkRewriter) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if l, ok := n.(*ast.Link); ok && entering {
			if m := pageLink.FindSubmatch(l.Destination); m != nil {
				l.Destination = []byte("/docs/" + string(m[1]) + string(m[2]))
			}
		}
		return ast.WalkContinue, nil
	})
}

// LLMs is /llms.txt: what Mainspring is, then every page with its summary
// and a link to its Markdown (https://llmstxt.org).
func (s *Set) LLMs(origin string) string {
	var b strings.Builder
	b.WriteString("# Mainspring\n\n> Mainspring pools the AI subscriptions you already pay for (Claude, Codex, Grok, Cursor, OpenCode Console and OpenAI-compatible APIs) behind one OpenAI- and Anthropic-compatible endpoint, with quota-aware routing.\n\n")
	b.WriteString("Each page below is Markdown. `" + origin + "/llms-full.txt` has every page in one file.\n")
	for _, g := range Order {
		b.WriteString("\n## " + g.Name + "\n\n")
		for _, slug := range g.Slugs {
			p := s.bySlug[slug]
			fmt.Fprintf(&b, "- [%s](%s/docs/%s.md): %s\n", p.Title, origin, p.Slug, p.Summary)
		}
	}
	return b.String()
}

// Full is /llms-full.txt: every page, in order.
func (s *Set) Full() string {
	var b strings.Builder
	for i, p := range s.Pages {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		b.WriteString(p.Source())
	}
	return b.String() + "\n"
}

// Source is the page as served at /docs/<page>.md: a title, the summary and the body.
func (p *Page) Source() string {
	return "# " + p.Title + "\n\n" + p.Summary + "\n\n" + p.Markdown + "\n"
}
