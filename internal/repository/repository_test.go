package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"github.com/Lmex89/home-cameras/internal/domain"
)

// openTestDB opens an in-memory SQLite database with the minimal schema
// needed by the repository tests.
func openTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	schema := `
CREATE TABLE cameras (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL, host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 80,
    username TEXT NOT NULL DEFAULT '',
    password TEXT NOT NULL DEFAULT '',
    profile_token TEXT, snapshot_url TEXT,
    interval_seconds INTEGER NOT NULL DEFAULT 60,
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
    error_message TEXT,
    archive_path TEXT
);
CREATE TABLE analysis_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    job_type TEXT NOT NULL DEFAULT 'yolo_detection',
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    error_message TEXT,
    requested_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP, finished_at TIMESTAMP
);
CREATE TABLE snapshot_analyses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    model_name TEXT NOT NULL,
    model_version TEXT NOT NULL DEFAULT '1.0',
    status TEXT NOT NULL DEFAULT 'pending',
    objects_json TEXT,
    person_count INTEGER NOT NULL DEFAULT 0,
    review_required BOOLEAN NOT NULL DEFAULT 0,
    review_reason TEXT,
    anomaly_score REAL, error_message TEXT,
    analyzed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(snapshot_id, model_name)
);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// TestCameraRepoCRUD exercises the full camera lifecycle.
func TestCameraRepoCRUD(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewCameraRepository(db)

	cam := &domain.Camera{Name: "Front", Host: "10.0.0.5", Port: 80, IntervalSeconds: 60, Enabled: true}
	if err := repo.Add(ctx, cam); err != nil {
		t.Fatalf("add: %v", err)
	}
	if cam.ID == 0 {
		t.Fatal("expected generated id")
	}
	if cam.CreatedAt.IsZero() {
		t.Fatal("expected server timestamp populated")
	}

	got, err := repo.GetByID(ctx, cam.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Front" {
		t.Fatalf("got %q", got.Name)
	}

	updated, err := repo.Update(ctx, cam.ID, map[string]any{"interval_seconds": 120, "enabled": false})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.IntervalSeconds != 120 || updated.Enabled {
		t.Fatalf("update not applied: %+v", updated)
	}

	enabled, err := repo.GetEnabled(ctx)
	if err != nil {
		t.Fatalf("enabled: %v", err)
	}
	if len(enabled) != 0 {
		t.Fatal("expected no enabled cameras")
	}

	deleted, err := repo.Delete(ctx, cam.ID)
	if err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
}

// TestSnapshotJobQueueRoundTrip verifies job lifecycle transitions.
func TestSnapshotJobQueueRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewAnalysisJobRepository(db)

	job := &domain.AnalysisJob{SnapshotID: 1, JobType: "yolo_detection", Status: "pending"}
	if err := repo.Add(ctx, job); err != nil {
		t.Fatalf("add: %v", err)
	}
	pending, err := repo.GetPending(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending: %v %v", pending, err)
	}
	if err := repo.MarkStarted(ctx, job.ID); err != nil {
		t.Fatalf("mark started: %v", err)
	}
	if err := repo.MarkCompleted(ctx, job.ID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	got, err := repo.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "completed" || got.Attempts != 1 {
		t.Fatalf("unexpected state: %+v", got)
	}
}

// TestSnapshotDateRange verifies day-boundary filtering.
func TestSnapshotDateRange(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewSnapshotRepository(db)

	day := time.Date(2026, 8, 7, 12, 0, 0, 0, time.Local)
	snap := &domain.Snapshot{
		CameraID: 1, CapturedAt: domain.NewSQLTime(day),
		ImagePath: "1/2026/08/07/120000.jpg", FileSize: 10, Status: "success",
	}
	if err := repo.Add(ctx, snap); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, err := repo.GetByCameraAndDate(ctx, 1, day)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(got))
	}
	other, err := repo.GetByCameraAndDate(ctx, 1, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("get other day: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("expected 0 snapshots for other day")
	}
}
