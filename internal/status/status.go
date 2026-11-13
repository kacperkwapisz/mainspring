// Package status follows providers' public status pages, so the panel can say
// when a failure is the provider's outage rather than something to fix here.
//
// Anthropic and Cursor publish Statuspage JSON and OpenAI publishes
// incident.io's; xAI's page names no component for the Grok API and OpenCode
// has none, so Grok and OpenCode aren't followed.
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

// Every is how often an open status page is read.
const Every = 2 * time.Minute

// Incident is one open incident that affects requests through Mainspring.
type Incident struct {
	Provider  domain.ProviderID `json:"provider"`
	Company   string            `json:"company"` // who runs the status page, like Anthropic
	Name      string            `json:"name"`
	Impact    string            `json:"impact"` // none, minor, major or critical
	Status    string            `json:"status"` // investigating, identified, monitoring
	Update    string            `json:"update,omitempty"`
	StartedAt int64             `json:"started_at"`
	UpdatedAt int64             `json:"updated_at"`
	URL       string            `json:"url"`
}

type page struct {
	provider   domain.ProviderID
	company    string
	url        string // Statuspage incidents JSON, or an incident.io page's /proxy/ summary
	incidentIO bool
	// components that matter to requests through Mainspring; an incident
	// touching only others (claude.ai connectors, the Console) is left out.
	components []string
}

var pages = []page{{
	provider:   domain.ProviderClaude,
	company:    "Anthropic",
	url:        "https://status.claude.com/api/v2/incidents/unresolved.json",
	components: []string{"Claude API", "Claude Code"},
}, {
	// OpenAI's Statuspage-style feed lists no components, so read incident.io's own.
	provider:   domain.ProviderCodex,
	company:    "OpenAI",
	url:        "https://status.openai.com/proxy/status.openai.com",
	incidentIO: true,
	components: []string{"Responses", "Codex API", "CLI", "Login"},
}, {
	provider:   domain.ProviderCursor,
	company:    "Cursor",
	url:        "https://status.cursor.com/api/v2/incidents.json",
	components: []string{"IDE", "CLI", "Third-party models"},
}}

// Watcher holds the open incidents.
type Watcher struct {
	mu       sync.RWMutex
	open     map[domain.ProviderID][]Incident
	client   *http.Client
	OnChange func(opened, resolved []Incident) // called outside the lock
}

func NewWatcher() *Watcher {
	return &Watcher{open: map[domain.ProviderID][]Incident{}, client: &http.Client{Timeout: 15 * time.Second}}
}

// Open lists open incidents, newest first. A nil Watcher has none.
func (w *Watcher) Open() []Incident {
	if w == nil {
		return []Incident{}
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := []Incident{}
	for _, list := range w.open {
		out = append(out, list...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// For returns the newest open incident for a provider.
func (w *Watcher) For(p domain.ProviderID) (Incident, bool) {
	if w == nil {
		return Incident{}, false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	list := w.open[p]
	if len(list) == 0 {
		return Incident{}, false
	}
	return list[0], true
}

// Check reads every page whose provider is in the pool. A page that can't be
// read keeps its last answer.
func (w *Watcher) Check(ctx context.Context, inPool func(domain.ProviderID) bool) {
	for _, pg := range pages {
		if inPool != nil && !inPool(pg.provider) {
			w.set(pg.provider, nil)
			continue
		}
		list, err := w.fetch(ctx, pg)
		if err != nil {
			continue
		}
		w.set(pg.provider, list)
	}
}

// Set replaces a provider's open incidents, as a check would. The demo uses it.
func (w *Watcher) Set(p domain.ProviderID, list []Incident) { w.set(p, list) }

func (w *Watcher) set(p domain.ProviderID, list []Incident) {
	w.mu.Lock()
	before := map[string]Incident{}
	for _, in := range w.open[p] {
		before[in.URL] = in
	}
	w.open[p] = list
	var opened, resolved []Incident
	now := map[string]bool{}
	for _, in := range list {
		now[in.URL] = true
		if _, had := before[in.URL]; !had {
			opened = append(opened, in)
		}
	}
	for url, in := range before {
		if !now[url] {
			resolved = append(resolved, in)
		}
	}
	changed := len(opened)+len(resolved) > 0 || !sameUpdates(before, list)
	cb := w.OnChange
	w.mu.Unlock()
	if changed && cb != nil {
		cb(opened, resolved)
	}
}

func sameUpdates(before map[string]Incident, list []Incident) bool {
	for _, in := range list {
		if b, ok := before[in.URL]; ok && b.UpdatedAt != in.UpdatedAt {
			return false
		}
	}
	return true
}

func (w *Watcher) fetch(ctx context.Context, pg page) ([]Incident, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pg.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", pg.url, res.StatusCode)
	}
	if pg.incidentIO {
		var body incidentIOPage
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			return nil, err
		}
		return parseIncidentIO(pg, body), nil
	}
	var body statuspage
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	return parse(pg, body), nil
}

type statuspage struct {
	Incidents []struct {
		Name       string    `json:"name"`
		Status     string    `json:"status"`
		Impact     string    `json:"impact"`
		Shortlink  string    `json:"shortlink"`
		CreatedAt  time.Time `json:"created_at"`
		UpdatedAt  time.Time `json:"updated_at"`
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
		Updates []struct {
			Body string `json:"body"`
		} `json:"incident_updates"`
	} `json:"incidents"`
}

func parse(pg page, body statuspage) []Incident {
	var out []Incident
	for _, in := range body.Incidents {
		if in.Status == "resolved" || in.Status == "postmortem" {
			continue
		}
		var names []string
		for _, c := range in.Components {
			names = append(names, c.Name)
		}
		if !pg.relevant(names) {
			continue
		}
		x := Incident{Provider: pg.provider, Company: pg.company, Name: in.Name, Impact: in.Impact, Status: in.Status,
			StartedAt: in.CreatedAt.UnixMilli(), UpdatedAt: in.UpdatedAt.UnixMilli(), URL: in.Shortlink}
		if x.URL == "" {
			x.URL = pg.home()
		}
		if len(in.Updates) > 0 {
			x.Update = strings.TrimSpace(in.Updates[0].Body)
		}
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

func (pg page) relevant(components []string) bool {
	for _, c := range components {
		for _, want := range pg.components {
			if c == want || strings.HasPrefix(c, want+" ") {
				return true
			}
		}
	}
	return false
}

// home is the status page itself.
func (pg page) home() string {
	u := pg.url
	if i := strings.Index(u, "/api/"); i > 0 {
		return u[:i]
	}
	if i := strings.Index(u, "/proxy/"); i > 0 {
		return u[:i]
	}
	return u
}

// incidentIOPage is the part of an incident.io status page summary we read.
type incidentIOPage struct {
	Summary struct {
		Components []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"components"`
		Ongoing []struct {
			ID          string    `json:"id"`
			Name        string    `json:"name"`
			Status      string    `json:"status"`
			PublishedAt time.Time `json:"published_at"`
			Affected    []struct {
				ComponentID string `json:"component_id"`
				Status      string `json:"current_status"`
			} `json:"affected_components"`
			Updates []struct {
				PublishedAt time.Time `json:"published_at"`
				Text        string    `json:"message_string"`
			} `json:"updates"`
		} `json:"ongoing_incidents"`
	} `json:"summary"`
}

func parseIncidentIO(pg page, body incidentIOPage) []Incident {
	names := map[string]string{}
	for _, c := range body.Summary.Components {
		names[c.ID] = c.Name
	}
	var out []Incident
	for _, in := range body.Summary.Ongoing {
		if in.Status == "resolved" {
			continue
		}
		var affected []string
		impact := "minor"
		for _, a := range in.Affected {
			affected = append(affected, names[a.ComponentID])
			switch a.Status {
			case "full_outage":
				impact = "critical"
			case "partial_outage":
				if impact != "critical" {
					impact = "major"
				}
			}
		}
		if !pg.relevant(affected) {
			continue
		}
		x := Incident{Provider: pg.provider, Company: pg.company, Name: in.Name, Impact: impact, Status: in.Status,
			StartedAt: in.PublishedAt.UnixMilli(), UpdatedAt: in.PublishedAt.UnixMilli(), URL: pg.home() + "/incidents/" + in.ID}
		for _, u := range in.Updates {
			if u.PublishedAt.UnixMilli() >= x.UpdatedAt {
				x.UpdatedAt, x.Update = u.PublishedAt.UnixMilli(), strings.TrimSpace(u.Text)
			}
		}
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}
