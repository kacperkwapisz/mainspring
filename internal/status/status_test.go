package status

import (
	"encoding/json"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

const sample = `{"incidents":[
 {"name":"Elevated errors on claude.ai, the Claude API, and Claude Code","status":"investigating","impact":"major","shortlink":"https://stspg.io/a","created_at":"2026-11-13T13:09:00Z","updated_at":"2026-11-13T13:20:00Z",
  "components":[{"name":"claude.ai"},{"name":"Claude API (api.anthropic.com)"},{"name":"Claude Code"}],
  "incident_updates":[{"body":"We are investigating reports of degraded performance."}]},
 {"name":"Elevated errors affecting the Microsoft 365 connector","status":"identified","impact":"major","shortlink":"https://stspg.io/b","created_at":"2026-11-12T17:52:00Z","updated_at":"2026-11-12T18:00:00Z",
  "components":[{"name":"claude.ai"},{"name":"Claude Cowork"}],"incident_updates":[]}
]}`

func TestParseKeepsIncidentsThatAffectRequests(t *testing.T) {
	var body statuspage
	if err := json.Unmarshal([]byte(sample), &body); err != nil {
		t.Fatal(err)
	}
	got := parse(pages[0], body)
	if len(got) != 1 || got[0].URL != "https://stspg.io/a" || got[0].Provider != domain.ProviderClaude || got[0].Update == "" {
		t.Fatalf("%+v", got)
	}
}

func TestSetReportsOpenedAndResolved(t *testing.T) {
	w := NewWatcher()
	var opened, resolved int
	w.OnChange = func(o, r []Incident) { opened += len(o); resolved += len(r) }
	a := Incident{Provider: domain.ProviderClaude, URL: "a", StartedAt: 1}
	w.set(domain.ProviderClaude, []Incident{a})
	w.set(domain.ProviderClaude, []Incident{a})
	w.set(domain.ProviderClaude, nil)
	if opened != 1 || resolved != 1 {
		t.Fatalf("opened %d resolved %d", opened, resolved)
	}
	if _, ok := w.For(domain.ProviderClaude); ok {
		t.Fatal("still open")
	}
	var nilW *Watcher
	if len(nilW.Open()) != 0 {
		t.Fatal("nil watcher")
	}
}
