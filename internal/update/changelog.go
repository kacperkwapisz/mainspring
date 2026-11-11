package update

import (
	"regexp"
	"strings"
)

// Release is one version's entry in CHANGELOG.md (Keep a Changelog style).
type Release struct {
	Version string   `json:"version"`
	Date    string   `json:"date,omitempty"`
	Items   []string `json:"items"` // one bullet each, light Markdown (`code`, **bold**)
}

var heading = regexp.MustCompile(`^## \[?v?([0-9][^\]\s]*)\]?(?:\s+-\s+(\S+))?`)

// ParseChangelog reads CHANGELOG.md, newest version first as written.
func ParseChangelog(md string) []Release {
	var out []Release
	var cur *Release
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if m := heading.FindStringSubmatch(line); m != nil {
			out = append(out, Release{Version: m[1], Date: m[2]})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "- "), strings.HasPrefix(line, "* "):
			cur.Items = append(cur.Items, strings.TrimSpace(line[2:]))
		case strings.HasPrefix(line, "  ") && len(cur.Items) > 0 && strings.TrimSpace(line) != "":
			cur.Items[len(cur.Items)-1] += " " + strings.TrimSpace(line) // wrapped bullet
		}
	}
	return out
}

// Section returns the Markdown body of one version's entry, for release notes.
func Section(md, version string) string {
	version = strings.TrimPrefix(version, "v")
	var b strings.Builder
	in := false
	for _, line := range strings.Split(md, "\n") {
		if m := heading.FindStringSubmatch(line); m != nil {
			if in {
				break
			}
			in = m[1] == version
			continue
		}
		if in {
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}
