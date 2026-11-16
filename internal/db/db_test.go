package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOldUsageRowsLoseTheirCacheFromPromptTokensOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	// A database from before this change: the tables, old-style rows and
	// user_version 0.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range migrations {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	rows := []struct {
		provider            string
		prompt, read, write int64
		wantPrompt          int64
	}{
		{"cursor", 1000, 600, 300, 100},
		{"cursor", 100, 600, 0, 0}, // never below zero
		{"codex", 1000, 600, 300, 400},
		{"grok", 500, 100, 0, 400},
		{"openai", 50, 80, 0, 0},
		{"claude", 1000, 600, 300, 1000},
		{"opencode", 1000, 600, 300, 1000},
	}
	for _, r := range rows {
		if _, err := raw.Exec(`INSERT INTO usage_events(ts, provider, model, inbound, prompt_tokens, cache_read_tokens, cache_write_tokens, status)
			VALUES (1, ?, 'm', 'openai.chat', ?, ?, ?, 'ok')`, r.provider, r.prompt, r.read, r.write); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	check := func(round int) {
		d, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		res, err := d.SQL.Query(`SELECT prompt_tokens FROM usage_events ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Close()
		for i := 0; res.Next(); i++ {
			var got int64
			_ = res.Scan(&got)
			if got != rows[i].wantPrompt {
				t.Errorf("start %d, %s row %d: prompt_tokens %d, want %d", round, rows[i].provider, i, got, rows[i].wantPrompt)
			}
		}
		var version int
		_ = d.SQL.QueryRow("PRAGMA user_version").Scan(&version)
		if version == 0 {
			t.Errorf("start %d: user_version %d", round, version)
		}
	}
	check(1)
	check(2) // a restart doesn't subtract again
}
