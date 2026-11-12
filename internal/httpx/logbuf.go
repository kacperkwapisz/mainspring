package httpx

import (
	"sync"
	"time"
)

// LogEntry is one line Mainspring logged, kept so the panel can show it.
type LogEntry struct {
	Seq   int64  `json:"seq"`
	TS    int64  `json:"ts"`    // unix ms
	Level string `json:"level"` // error, info or debug
	Line  string `json:"line"`
}

// logKeep is how many recent lines the panel can read. Older lines stay in the
// server log only.
const logKeep = 1000

var (
	bufMu   sync.Mutex
	bufRing = make([]LogEntry, 0, logKeep)
	bufSeq  int64
	bufHook func()
)

// SetLogHook is called after each kept line, outside any lock. The app uses it
// to tell open panels that the log grew.
func SetLogHook(f func()) {
	bufMu.Lock()
	bufHook = f
	bufMu.Unlock()
}

func keepLine(level logLevel, line string) {
	name := "info"
	switch level {
	case levelError:
		name = "error"
	case levelDebug:
		name = "debug"
	}
	bufMu.Lock()
	bufSeq++
	e := LogEntry{Seq: bufSeq, TS: time.Now().UnixMilli(), Level: name, Line: line}
	if len(bufRing) < logKeep {
		bufRing = append(bufRing, e)
	} else {
		copy(bufRing, bufRing[1:])
		bufRing[len(bufRing)-1] = e
	}
	hook := bufHook
	bufMu.Unlock()
	if hook != nil {
		hook()
	}
}

// RecentLog returns kept lines newer than seq, oldest first.
func RecentLog(after int64) []LogEntry {
	bufMu.Lock()
	defer bufMu.Unlock()
	out := []LogEntry{}
	for _, e := range bufRing {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out
}
