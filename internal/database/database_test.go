package database

import (
	"context"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"github.com/Lmex89/home-cameras/internal/config"
)

// TestOpenAppliesSchema verifies Open creates the full schema and that
// reopening an existing database is idempotent.
func TestOpenAppliesSchema(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir()}
	ctx := context.Background()

	db, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var tables []string
	if err := db.Select(&tables,
		"SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"); err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, n := range tables {
		have[n] = true
	}
	for _, want := range []string{"cameras", "snapshots", "analysis_jobs", "snapshot_analyses"} {
		if !have[want] {
			t.Errorf("table %s missing: %v", want, tables)
		}
	}

	// Reopen must succeed and keep the schema.
	db2, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	db2.Close()

	if err := PingWithTimeout(ctx, db); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

// TestOpenMigratesLegacyDB verifies the interval_minutes / archive_path
// / analysis-tables migrations on a pre-legacy database.
func TestOpenMigratesLegacyDB(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cameras.db"
	raw, err := sqlx.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
CREATE TABLE cameras (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL, host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 80,
    username TEXT NOT NULL DEFAULT '', password TEXT NOT NULL DEFAULT '',
    profile_token TEXT, snapshot_url TEXT,
    interval_minutes INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id INTEGER NOT NULL,
    captured_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    image_path TEXT NOT NULL,
    file_size INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'success',
    error_message TEXT
);
INSERT INTO cameras (name, host, interval_minutes) VALUES ('Legacy', '10.0.0.9', 2);
INSERT INTO snapshots (camera_id, captured_at, image_path) VALUES (1, '2026-01-01 00:00:00', '1.jpg');
`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	cfg := config.Config{DataDir: dir}
	ctx := context.Background()
	db, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("migrate open: %v", err)
	}
	defer db.Close()

	// interval_minutes -> interval_seconds with *60 conversion.
	var interval int
	if err := db.Get(&interval, "SELECT interval_seconds FROM cameras WHERE name='Legacy'"); err != nil {
		t.Fatalf("interval_seconds column: %v", err)
	}
	if interval != 120 {
		t.Fatalf("interval_seconds = %d want 120", interval)
	}

	// archive_path column added.
	cols := map[string]bool{}
	rows, err := db.QueryxContext(ctx, "PRAGMA table_info(snapshots)")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	rows.Close()
	if !cols["archive_path"] {
		t.Fatal("archive_path column missing after migration")
	}

	// Analysis tables created.
	var n int
	if err := db.Get(&n, "SELECT COUNT(*) FROM analysis_jobs"); err != nil {
		t.Fatalf("analysis_jobs missing: %v", err)
	}
	if err := db.Get(&n, "SELECT COUNT(*) FROM snapshot_analyses"); err != nil {
		t.Fatalf("snapshot_analyses missing: %v", err)
	}
}

// TestSplitStatements verifies statement splitting skips empties.
func TestSplitStatements(t *testing.T) {
	got := splitStatements("a; ; b;\n;c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("got %v", got)
	}
	if len(splitStatements(";;;")) != 0 {
		t.Fatal("expected no statements")
	}
	if !strings.HasPrefix(schemaSQL, "CREATE TABLE") {
		t.Fatal("embedded schema missing")
	}
}

// TestPingWithTimeout verifies health ping against a live DB.
func TestPingWithTimeout(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir()}
	db, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := PingWithTimeout(context.Background(), db); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
