package httpx

import (
	"io"
	"testing"
)

func TestRecentLogKeepsTheLastLinesInOrder(t *testing.T) {
	defer SetLogOutput(io.Discard)()
	SetLogLevel("info")
	calls := 0
	SetLogHook(func() { calls++ })
	defer SetLogHook(nil)
	start := RecentLog(0)
	var from int64
	if len(start) > 0 {
		from = start[len(start)-1].Seq
	}
	for i := 0; i < logKeep+5; i++ {
		Logf("line %d", i)
	}
	logln(levelDebug, "hidden at info level")
	all := RecentLog(0)
	if len(all) != logKeep {
		t.Fatalf("kept %d", len(all))
	}
	if all[len(all)-1].Line != "mainspring line 1004" || all[0].Line != "mainspring line 5" {
		t.Fatalf("first %q last %q", all[0].Line, all[len(all)-1].Line)
	}
	if calls != logKeep+5 {
		t.Fatalf("hook ran %d times", calls)
	}
	newer := RecentLog(all[len(all)-2].Seq)
	if len(newer) != 1 || newer[0].Seq <= from {
		t.Fatalf("%+v", newer)
	}
	Fail{Kind: KindUpstream, Status: 502, Message: "boom"}.Log()
	if last := RecentLog(0); last[len(last)-1].Level != "error" {
		t.Fatalf("%+v", last[len(last)-1])
	}
}
