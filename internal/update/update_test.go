package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mainspring "github.com/kacperkwapisz/mainspring"
)

func TestNewerFollowsSemverPrecedence(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.0.0-rc.10", "1.0.0-rc.9", true}, // numeric, not text
		{"1.0.0", "1.0.0-rc.12", true},      // a release beats its candidates
		{"1.0.0-rc.12", "1.0.0", false},
		{"v1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"0.10.0", "0.9.4", true},
		{"dev", "1.0.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestShippedChangelogParses(t *testing.T) {
	releases := ParseChangelog(mainspring.Changelog)
	if len(releases) == 0 || releases[0].Version == "" || len(releases[0].Items) == 0 {
		t.Fatalf("CHANGELOG.md needs a newest entry with bullets: %+v", releases)
	}
	for i := 1; i < len(releases); i++ {
		if !Newer(releases[i-1].Version, releases[i].Version) {
			t.Fatalf("entries out of order: %s before %s", releases[i-1].Version, releases[i].Version)
		}
	}
	if Section(mainspring.Changelog, "v"+releases[0].Version) == "" {
		t.Fatal("Section can't find the newest entry")
	}
}

func TestCheckPicksTheRightChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"tag_name":"v1.1.0-rc.1","prerelease":true,"html_url":"https://example.com/rc","body":"- rc"},
			{"tag_name":"v1.0.1","prerelease":false,"html_url":"https://example.com/101","body":"- **Fix** for x"},
			{"tag_name":"v1.0.0","prerelease":false,"html_url":"https://example.com/100"},
			{"tag_name":"v2.0.0","draft":true}
		]`))
	}))
	defer srv.Close()
	ReleasesURL = srv.URL

	stable := NewChecker("1.0.0", "docker")
	if err := stable.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := stable.Status(true); !s.Available || s.Latest.Version != "1.0.1" || s.Latest.Notes != "- **Fix** for x" {
		t.Fatalf("stable install should see 1.0.1 only: %+v", s.Latest)
	}
	candidate := NewChecker("1.0.1-rc.2", "umbrel")
	_ = candidate.Check(context.Background())
	if s := candidate.Status(true); !s.Available || s.Latest.Version != "1.1.0-rc.1" {
		t.Fatalf("a release candidate follows candidates: %+v", s.Latest)
	}
	current := NewChecker("1.1.0-rc.1", "binary")
	_ = current.Check(context.Background())
	if s := current.Status(true); s.Available {
		t.Fatalf("up to date: %+v", s)
	}
	dev := NewChecker("dev", "binary")
	_ = dev.Check(context.Background())
	if s := dev.Status(true); s.Available || s.CheckedAt != 0 {
		t.Fatalf("a dev build never checks: %+v", s)
	}
}
