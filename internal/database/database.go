// Package database opens the SQLite database with WAL pragmas, applies
// the embedded DDL schema, and runs the same forward migrations the
// legacy Python app performed (interval_minutes -> interval_seconds,
// archive_path column, analysis tables).
package database

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"github.com/Lmex89/home-cameras/internal/config"
)

//go:embed schema.sql
var schemaSQL string

// Open connects to the SQLite database, applies schema and migrations,
// and returns a pool configured for concurrent access (WAL mode).
func Open(ctx context.Context, cfg config.Config) (*sqlx.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)", cfg.DBPath())
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	return db, nil
}

// migrate applies the schema (idempotent CREATE IF NOT EXISTS) and then
// the legacy-compatible migrations. Order matters: the schema must exist
// before migrations inspect tables, while existing databases are left
// untouched by the CREATE statements so their data survives.
func migrate(ctx context.Context, db *sqlx.DB) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, stmt := range splitStatements(schemaSQL) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("execute schema statement: %w", err)
		}
	}
	if err := migrateIntervalColumn(ctx, tx); err != nil {
		return err
	}
	if err := migrateArchiveColumn(ctx, tx); err != nil {
		return err
	}
	if err := migrateAnalysisTables(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// splitStatements splits a SQL file into individual statements.
func splitStatements(raw string) []string {
	var out []string
	for _, stmt := range strings.Split(raw, ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// columnNames returns the column names of a table.
func columnNames(ctx context.Context, q sqlx.QueryerContext, table string) (map[string]bool, error) {
	rows, err := q.QueryxContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		// PRAGMA table_info returns (cid, name, type, notnull, dflt_value, pk).
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func migrateIntervalColumn(ctx context.Context, tx *sqlx.Tx) error {
	cols, err := columnNames(ctx, tx, "cameras")
	if err != nil {
		return err
	}
	if !cols["interval_minutes"] {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE cameras RENAME COLUMN interval_minutes TO interval_seconds"); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE cameras SET interval_seconds = interval_seconds * 60")
	return err
}

func migrateArchiveColumn(ctx context.Context, tx *sqlx.Tx) error {
	cols, err := columnNames(ctx, tx, "snapshots")
	if err != nil {
		return err
	}
	if cols["archive_path"] {
		return nil
	}
	_, err = tx.ExecContext(ctx, "ALTER TABLE snapshots ADD COLUMN archive_path TEXT")
	return err
}

func migrateAnalysisTables(ctx context.Context, tx *sqlx.Tx) error {
	var names []string
	if err := tx.SelectContext(ctx, &names, "SELECT name FROM sqlite_master WHERE type='table'"); err != nil {
		return err
	}
	tables := map[string]bool{}
	for _, n := range names {
		tables[n] = true
	}
	if !tables["analysis_jobs"] {
		if _, err := tx.ExecContext(ctx, analysisJobsDDL); err != nil {
			return err
		}
	}
	if !tables["snapshot_analyses"] {
		if _, err := tx.ExecContext(ctx, snapshotAnalysesDDL); err != nil {
			return err
		}
	}
	return nil
}

const analysisJobsDDL = `
CREATE TABLE IF NOT EXISTS analysis_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    job_type TEXT NOT NULL DEFAULT 'yolo_detection',
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    error_message TEXT,
    requested_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    FOREIGN KEY (snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE
)`

const snapshotAnalysesDDL = `
CREATE TABLE IF NOT EXISTS snapshot_analyses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    model_name TEXT NOT NULL,
    model_version TEXT NOT NULL DEFAULT '1.0',
    status TEXT NOT NULL DEFAULT 'pending',
    objects_json TEXT,
    person_count INTEGER NOT NULL DEFAULT 0,
    review_required BOOLEAN NOT NULL DEFAULT 0,
    review_reason TEXT,
    anomaly_score REAL,
    error_message TEXT,
    analyzed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE,
    UNIQUE(snapshot_id, model_name)
)`

// PingWithTimeout verifies DB health with a bounded deadline.
func PingWithTimeout(ctx context.Context, db *sqlx.DB) error {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var one int
	return db.GetContext(cctx, &one, "SELECT 1")
}
