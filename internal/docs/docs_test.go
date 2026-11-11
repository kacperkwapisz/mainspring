package docs

import (
	"regexp"
	"strings"
	"testing"

	mainspring "github.com/kacperkwapisz/mainspring"
)

func TestShippedDocsLoadAndLinkToRealPlaces(t *testing.T) {
	set, err := Load(mainspring.Docs)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]map[string]bool{}
	idRe := regexp.MustCompile(`id="([^"]+)"`)
	for _, p := range set.Pages {
		ids[p.Slug] = map[string]bool{}
		for _, m := range idRe.FindAllStringSubmatch(p.HTML, -1) {
			ids[p.Slug][m[1]] = true
		}
	}
	linkRe := regexp.MustCompile(`href="/docs/([a-z0-9-]+)(?:#([a-z0-9-]+))?"`)
	for _, p := range set.Pages {
		if strings.Contains(p.HTML, `.md"`) && regexp.MustCompile(`href="[a-z0-9-]+\.md`).MatchString(p.HTML) {
			t.Errorf("%s: a page link wasn't rewritten to /docs/", p.Slug)
		}
		for _, m := range linkRe.FindAllStringSubmatch(p.HTML, -1) {
			target, ok := ids[m[1]]
			if !ok {
				t.Errorf("%s links to a missing page %q", p.Slug, m[1])
				continue
			}
			if m[2] != "" && !target[m[2]] {
				t.Errorf("%s links to %s#%s, which has no such heading", p.Slug, m[1], m[2])
			}
		}
	}
	llms := set.LLMs("https://ms.example")
	for _, p := range set.Pages {
		if !strings.Contains(llms, "(https://ms.example/docs/"+p.Slug+".md): "+p.Summary) {
			t.Errorf("llms.txt is missing %s", p.Slug)
		}
	}
	if !strings.HasPrefix(set.Full(), "# Get started\n") {
		t.Fatal("llms-full.txt should start with the first page")
	}
}
