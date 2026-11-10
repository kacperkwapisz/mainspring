package app

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/live"
)

const (
	// eventsMinGap keeps a busy proxy from making panels reload more than once a second.
	eventsMinGap = time.Second
	// eventsPing lets the panel notice a connection that went quiet (a proxy
	// that buffers, a laptop that slept) and fall back to polling.
	eventsPing = 20 * time.Second
)

// streamChanges tells the panel which topics changed, as server-sent events:
// "hello" on connect, "change" with {"topics": [...]}, and "ping" to show the
// stream is alive. The panel reloads what it shows; no data rides the stream.
func streamChanges(w http.ResponseWriter, r *http.Request, bus *live.Bus) {
	sub, drop := bus.Subscribe()
	defer drop()
	ctx := r.Context()
	out := make(chan string)
	send := func(line string) bool {
		select {
		case out <- line:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		defer close(out)
		if !send("retry: 3000\nevent: hello\ndata: {}\n\n") {
			return
		}
		ping := time.NewTicker(eventsPing)
		defer ping.Stop()
		var last time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-ping.C:
				if !send("event: ping\ndata: {}\n\n") {
					return
				}
			case <-sub.Wake:
				if wait := eventsMinGap - time.Since(last); wait > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(wait):
					}
				}
				topics := sub.Take()
				if len(topics) == 0 {
					continue
				}
				sort.Strings(topics)
				raw, _ := json.Marshal(map[string]any{"topics": topics})
				if !send("event: change\ndata: " + string(raw) + "\n\n") {
					return
				}
				last = time.Now()
			}
		}
	}()
	httpx.StreamSSE(w, r, out)
}
