package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS settings (
     key TEXT PRIMARY KEY,
     value TEXT NOT NULL
   )`,
	`CREATE TABLE IF NOT EXISTS panel_auth (
     id INTEGER PRIMARY KEY CHECK (id = 1),
     password_hash TEXT NOT NULL,
     must_reset INTEGER NOT NULL DEFAULT 1,
     created_at INTEGER NOT NULL
   )`,
	`CREATE TABLE IF NOT EXISTS local_keys (
     id TEXT PRIMARY KEY,
     name TEXT NOT NULL,
     prefix TEXT NOT NULL,
     hash TEXT NOT NULL UNIQUE,
     providers TEXT,
     models TEXT,
     daily_cap INTEGER,
     revoked INTEGER NOT NULL DEFAULT 0,
     created_at INTEGER NOT NULL
   )`,
	`CREATE TABLE IF NOT EXISTS credentials (
     id TEXT PRIMARY KEY,
     provider TEXT NOT NULL,
     label TEXT NOT NULL,
     blob TEXT NOT NULL,
     expires_at INTEGER,
     created_at INTEGER NOT NULL,
     updated_at INTEGER NOT NULL
   )`,
	`CREATE TABLE IF NOT EXISTS usage_events (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
     ts INTEGER NOT NULL,
     key_id TEXT,
     provider TEXT NOT NULL,
     model TEXT NOT NULL,
     inbound TEXT NOT NULL,
     prompt_tokens INTEGER NOT NULL DEFAULT 0,
     completion_tokens INTEGER NOT NULL DEFAULT 0,
     cache_read_tokens INTEGER NOT NULL DEFAULT 0,
     cache_write_tokens INTEGER NOT NULL DEFAULT 0,
     latency_ms INTEGER NOT NULL DEFAULT 0,
     status TEXT NOT NULL,
     usd REAL NOT NULL DEFAULT 0
   )`,
	`CREATE INDEX IF NOT EXISTS usage_events_ts ON usage_events(ts)`,
	`CREATE TABLE IF NOT EXISTS prices (
     provider TEXT NOT NULL,
     model TEXT NOT NULL,
     input REAL,
     output REAL,
     cache_read REAL,
     cache_write REAL,
     tiers TEXT,
     fetched_at INTEGER NOT NULL,
     PRIMARY KEY (provider, model)
   )`,
	`CREATE TABLE IF NOT EXISTS auth_credentials (
     id TEXT PRIMARY KEY,
     name TEXT NOT NULL,
     kind TEXT NOT NULL,
     rp_id TEXT NOT NULL,
     credential_json TEXT NOT NULL,
     created_at INTEGER NOT NULL,
     last_used_at INTEGER
   )`,
	`CREATE TABLE IF NOT EXISTS auth_setup (
     id INTEGER PRIMARY KEY CHECK (id = 1),
     code_hash TEXT NOT NULL,
     created_at INTEGER NOT NULL
   )`,
}

// dataMigrations rewrite existing rows. Each runs once: SQLite's
// user_version counts how many have been applied, and a migration and its
// version bump share one transaction. Append only; never reorder.
var dataMigrations = []string{
	// Rows from before the meter read the provider's own usage stored
	// OpenAI-style providers' prompt_tokens with the cached tokens included.
	// prompt_tokens is uncached input now, so take the cache out. Cursor
	// counted cache writes as input too. Claude was already uncached, and
	// OpenCode mixed conventions per model family, so both are left alone.
	`UPDATE usage_events SET prompt_tokens = MAX(prompt_tokens - cache_read_tokens - cache_write_tokens, 0)
	   WHERE provider = 'cursor'`,
	`UPDATE usage_events SET prompt_tokens = MAX(prompt_tokens - cache_read_tokens, 0)
	   WHERE provider IN ('codex', 'grok', 'openai')`,
}

// runDataMigrations applies the data migrations this database hasn't had.
func runDataMigrations(sqlDB *sql.DB) error {
	for {
		tx, err := sqlDB.Begin()
		if err != nil {
			return err
		}
		var version int
		if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			_ = tx.Rollback()
			return err
		}
		if version >= len(dataMigrations) {
			return tx.Rollback()
		}
		if _, err := tx.Exec(dataMigrations[version]); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
}

type DB struct {
	SQL *sql.DB
}

func Open(path string) (*DB, error) {
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
		}
	}
	dsn := path
	if path == ":memory:" {
		dsn = "file:hub?mode=memory&cache=shared"
	}
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		sqlDB.SetMaxOpenConns(1)
	}
	if _, err := sqlDB.Exec("PRAGMA journal_mode = WAL"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if _, err := sqlDB.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	for _, sqlStmt := range migrations {
		if _, err := sqlDB.Exec(sqlStmt); err != nil {
			sqlDB.Close()
			return nil, err
		}
	}
	if err := runDataMigrations(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return &DB{SQL: sqlDB}, nil
}

func (d *DB) Close() error {
	if d == nil || d.SQL == nil {
		return nil
	}
	return d.SQL.Close()
}
