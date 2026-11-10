package app

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

func TestEventsStreamTellsThePanelWhatChanged(t *testing.T) {
	b := bootAPI(t)
	srv := httptest.NewServer(b.Handler)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	authPanel(t, b, req)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status %d, type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	lines := bufio.NewScanner(res.Body)
	next := func(event string) string {
		t.Helper()
		seen := ""
		for lines.Scan() {
			l := lines.Text()
			if strings.HasPrefix(l, "event: ") {
				seen = strings.TrimPrefix(l, "event: ")
			}
			if strings.HasPrefix(l, "data: ") && seen == event {
				return strings.TrimPrefix(l, "data: ")
			}
		}
		t.Fatalf("stream ended before %q: %v", event, lines.Err())
		return ""
	}
	next("hello")
	saveCred(t, b, domain.ProviderClaude, "a@example.com")
	if data := next("change"); !strings.Contains(data, `"pool"`) {
		t.Fatalf("account change: %s", data)
	}
	_ = b.Hub.Settings.Set("routing.staleMinutes", "30")
	if data := next("change"); !strings.Contains(data, `"settings"`) {
		t.Fatalf("settings change: %s", data)
	}
}

func TestEventsNeedAPanelSession(t *testing.T) {
	b := bootAPI(t)
	res := httptest.NewRecorder()
	b.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/events", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", res.Code)
	}
}
