package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"
)

// FetchPage downloads a public web page and returns it as plain text with
// light Markdown (headings, links, lists). Addresses on the local network,
// loopback and link-local ranges are refused, so a key can't reach other
// services on the machine Mainspring runs on.
func FetchPage(ctx context.Context, rawURL string, maxChars int) (title, text string, err error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", errors.New("give a full http:// or https:// address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Mainspring web_fetch)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.5")
	res, err := publicClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", "", fmt.Errorf("the page answered %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return "", "", err
	}
	mediaType, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	switch {
	case mediaType == "" || strings.Contains(mediaType, "html"):
		title, text = htmlToText(string(raw))
	case strings.HasPrefix(mediaType, "text/") || strings.Contains(mediaType, "json") || strings.Contains(mediaType, "xml"):
		text = string(raw)
	default:
		return "", "", fmt.Errorf("can't read %s pages", mediaType)
	}
	text = strings.TrimSpace(text)
	if maxChars > 0 && len(text) > maxChars {
		text = text[:maxChars] + "\n\n[The page goes on; it was cut here.]"
	}
	return title, text, nil
}

var publicClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, Control: refusePrivate}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// refusePrivate runs on every connection, after DNS, so a public name that
// resolves to a private address is refused too.
func refusePrivate(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || cgnat.Contains(ip) {
		return fmt.Errorf("%s is on a private network", host)
	}
	return nil
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)} // Tailscale and carrier NAT

// htmlToText keeps a page's readable text: headings, paragraphs, list items,
// links and code, without scripts, styles or navigation chrome.
func htmlToText(src string) (title, text string) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", src
	}
	var b strings.Builder
	skip := map[string]bool{"script": true, "style": true, "noscript": true, "svg": true, "nav": true, "footer": true, "header": true, "form": true, "iframe": true, "template": true}
	block := map[string]bool{"p": true, "div": true, "section": true, "article": true, "main": true, "br": true, "tr": true, "table": true, "blockquote": true, "pre": true}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := n.Data
			if skip[tag] {
				return
			}
			if tag == "title" && n.FirstChild != nil && title == "" {
				title = strings.TrimSpace(n.FirstChild.Data)
				return
			}
			switch {
			case len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6':
				b.WriteString("\n\n" + strings.Repeat("#", int(tag[1]-'0')) + " ")
			case tag == "li":
				b.WriteString("\n- ")
			case tag == "a":
				href := attr(n, "href")
				var inner strings.Builder
				collectText(n, &inner)
				label := strings.Join(strings.Fields(inner.String()), " ")
				if label != "" && strings.HasPrefix(href, "http") {
					b.WriteString("[" + label + "](" + href + ")")
				} else {
					b.WriteString(label)
				}
				return
			case tag == "code" && (n.Parent == nil || n.Parent.Data != "pre"):
				var inner strings.Builder
				collectText(n, &inner)
				b.WriteString("`" + inner.String() + "`")
				return
			case block[tag]:
				b.WriteString("\n\n")
			}
		}
		if n.Type == html.TextNode {
			if t := strings.Join(strings.Fields(n.Data), " "); t != "" {
				if b.Len() > 0 && !strings.HasSuffix(b.String(), " ") && !strings.HasSuffix(b.String(), "\n") {
					b.WriteString(" ")
				}
				b.WriteString(t)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	lines := strings.Split(b.String(), "\n")
	var out []string
	blank := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || l == "-" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, l)
		blank = false
	}
	return title, strings.Join(out, "\n")
}

func collectText(n *html.Node, b *strings.Builder) {
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, b)
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
