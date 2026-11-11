// Package update tells the panel what changed in this version and whether a
// newer release is out.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ReleasesURL lists Mainspring's GitHub releases, newest first.
var ReleasesURL = "https://api.github.com/repos/kacperkwapisz/mainspring/releases?per_page=20"

// Latest is the newest release this install should move to.
type Latest struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	PublishedAt string `json:"published_at,omitempty"`
	Notes       string `json:"notes,omitempty"` // Markdown, from the release body
}

// Status is what the panel shows about updates.
type Status struct {
	Current   string `json:"current"`
	Install   string `json:"install"` // umbrel, docker or binary
	Enabled   bool   `json:"enabled"`
	CheckedAt int64  `json:"checked_at,omitempty"`
	Available bool   `json:"available"`
	// LatestVersion is the newest release on this install's channel, whether or not it's newer.
	LatestVersion string  `json:"latest_version,omitempty"`
	Latest        *Latest `json:"latest,omitempty"`
	Error         string  `json:"error,omitempty"`
}

// Checker asks GitHub for new releases and remembers the answer.
type Checker struct {
	Current string
	Install string
	Client  *http.Client

	mu     sync.Mutex
	latest *Latest
	at     int64
	err    string
}

// NewChecker builds a checker for this binary. install is MAINSPRING_INSTALL;
// empty means detect a container, else a plain binary.
func NewChecker(current, install string) *Checker {
	if install == "" {
		install = "binary"
		if _, err := os.Stat("/.dockerenv"); err == nil {
			install = "docker"
		}
	}
	return &Checker{Current: current, Install: install, Client: &http.Client{Timeout: 15 * time.Second}}
}

// Status reports the last check. enabled is the panel setting.
func (c *Checker) Status(enabled bool) Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{Current: c.Current, Install: c.Install, Enabled: enabled, CheckedAt: c.at, Error: c.err}
	if c.latest != nil {
		s.LatestVersion = c.latest.Version
	}
	if c.latest != nil && Newer(c.latest.Version, c.Current) {
		s.Available, s.Latest = true, c.latest
	}
	return s
}

// Check asks GitHub now. A development build (no release version) is never
// behind. Stable installs only move to stable releases; a release candidate
// also moves to newer release candidates.
func (c *Checker) Check(ctx context.Context) error {
	if _, _, ok := split(c.Current); !ok {
		return nil
	}
	latest, err := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = time.Now().UnixMilli()
	if err != nil {
		c.err = "Couldn't reach GitHub: " + err.Error()
		return err
	}
	c.err, c.latest = "", latest
	return nil
}

func (c *Checker) fetch(ctx context.Context) (*Latest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "mainspring/"+c.Current)
	res, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %d", res.StatusCode)
	}
	var releases []struct {
		Tag         string `json:"tag_name"`
		URL         string `json:"html_url"`
		Body        string `json:"body"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
		PublishedAt string `json:"published_at"`
	}
	if err := json.NewDecoder(res.Body).Decode(&releases); err != nil {
		return nil, err
	}
	wantPre := Prerelease(c.Current)
	var best *Latest
	for _, r := range releases {
		v := strings.TrimPrefix(r.Tag, "v")
		if r.Draft || (r.Prerelease && !wantPre) {
			continue
		}
		if _, _, ok := split(v); !ok {
			continue
		}
		if best == nil || Newer(v, best.Version) {
			best = &Latest{Version: v, URL: r.URL, PublishedAt: r.PublishedAt, Notes: strings.TrimSpace(r.Body)}
		}
	}
	return best, nil
}
