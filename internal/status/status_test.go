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

const openaiSample = `{"summary":{"components":[{"id":"r","name":"Responses"},{"id":"m","name":"Connectors/Apps"}],
 "ongoing_incidents":[
  {"id":"inc1","name":"Elevated errors on Responses","status":"investigating","published_at":"2026-11-13T10:00:00Z",
   "affected_components":[{"component_id":"r","current_status":"partial_outage"}],
   "updates":[{"published_at":"2026-11-13T10:00:00Z","message_string":"Looking"},{"published_at":"2026-11-13T10:20:00Z","message_string":"Found it"}]},
  {"id":"inc2","name":"Microsoft 365 connector","status":"monitoring","published_at":"2026-11-12T18:29:00Z",
   "affected_components":[{"component_id":"m","current_status":"degraded_performance"}],"updates":[]}]}}`

func TestParseIncidentIOKeepsCodexIncidents(t *testing.T) {
	var body incidentIOPage
	if err := json.Unmarshal([]byte(openaiSample), &body); err != nil {
		t.Fatal(err)
	}
	got := parseIncidentIO(pages[1], body)
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	in := got[0]
	if in.Provider != domain.ProviderCodex || in.Company != "OpenAI" || in.Impact != "major" || in.Update != "Found it" || in.URL != "https://status.openai.com/incidents/inc1" {
		t.Fatalf("%+v", in)
	}
}

func TestStatuspageFeedsSkipResolvedAndOtherComponents(t *testing.T) {
	var body statuspage
	_ = json.Unmarshal([]byte(`{"incidents":[
	 {"name":"IDE down","status":"resolved","components":[{"name":"IDE"}]},
	 {"name":"Grok Bot degraded","status":"investigating","components":[{"name":"Grok Bot"}]},
	 {"name":"Third-party models failing","status":"identified","shortlink":"https://stspg.io/c","components":[{"name":"Third-party models"}]}]}`), &body)
	got := parse(pages[2], body)
	if len(got) != 1 || got[0].Name != "Third-party models failing" || got[0].Company != "Cursor" {
		t.Fatalf("%+v", got)
	}
	if pages[2].home() != "https://status.cursor.com" || pages[1].home() != "https://status.openai.com" {
		t.Fatal(pages[2].home(), pages[1].home())
	}
}
