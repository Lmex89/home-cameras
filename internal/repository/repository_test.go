package repository

import (
	"context"
	"errors"
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

// TestSnapshotRepoAggregations covers the dashboard/report queries.
func TestSnapshotRepoAggregations(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewSnapshotRepository(db)

	day := time.Date(2026, 8, 7, 12, 0, 0, 0, time.Local)
	makeSnap := func(camID int64, at time.Time, status, path string) *domain.Snapshot {
		s := &domain.Snapshot{CameraID: camID, CapturedAt: domain.NewSQLTime(at),
			ImagePath: path, FileSize: 5, Status: status}
		if err := repo.Add(ctx, s); err != nil {
			t.Fatalf("add: %v", err)
		}
		return s
	}
	ok1 := makeSnap(1, day, "success", "1.jpg")
	ok2 := makeSnap(1, day.Add(time.Hour), "success", "2.jpg")
	fail := makeSnap(2, day.Add(-time.Hour), "error", "")

	// Last per camera (only successes).
	lasts, err := repo.GetLastForAllCameras(ctx, []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("lasts: %v", err)
	}
	if lasts[1] == nil || lasts[1].ID != ok2.ID {
		t.Fatalf("last for cam 1 = %+v", lasts[1])
	}
	if _, ok := lasts[2]; ok {
		t.Fatal("failed snapshot must not count as last")
	}
	if lasts[3] != nil {
		t.Fatal("no snapshots for camera 3")
	}
	empty, err := repo.GetLastForAllCameras(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty ids: %v %v", empty, err)
	}

	// By date across cameras.
	byDate, err := repo.GetByDate(ctx, day)
	if err != nil || len(byDate) != 3 {
		t.Fatalf("by date: %v %v", byDate, err)
	}

	// Successful-with-images only.
	good, err := repo.GetAllSuccessfulWithImages(ctx)
	if err != nil || len(good) != 2 {
		t.Fatalf("good: %v %v", good, err)
	}

	// Last by camera.
	last, err := repo.GetLastByCamera(ctx, 1)
	if err != nil || last.ID != ok2.ID {
		t.Fatalf("last: %v %v", last, err)
	}
	if _, err := repo.GetLastByCamera(ctx, 3); err == nil {
		t.Fatal("expected error for camera without snapshots")
	}

	// MarkArchivePathBatch + count by zip.
	if n, err := repo.MarkArchivedBatch(ctx, []int64{ok1.ID}, "snapshots/1/x.zip::1.jpg"); err != nil || n != 1 {
		t.Fatalf("mark batch: %v %v", n, err)
	}
	if n, err := repo.MarkArchivedBatch(ctx, nil, "x"); err != nil || n != 0 {
		t.Fatalf("empty mark batch: %v %v", n, err)
	}
	cnt, err := repo.CountByArchiveZip(ctx, "snapshots/1/x.zip")
	if err != nil || cnt != 1 {
		t.Fatalf("count by zip: %v %v", cnt, err)
	}

	// UpdateArchivePath.
	if err := repo.UpdateArchivePath(ctx, ok2.ID, "snapshots/1/y.zip::2.jpg"); err != nil {
		t.Fatal(err)
	}
	refreshed, err := repo.GetByID(ctx, ok2.ID)
	if err != nil || refreshed.ArchivePath == nil {
		t.Fatalf("refresh: %v %v", refreshed, err)
	}

	// GetOldUnarchived excludes archived and fresh rows.
	old, err := repo.GetOldUnarchived(ctx, day.Add(30*time.Minute))
	if err != nil || len(old) != 1 || old[0].ID != fail.ID {
		t.Fatalf("old unarchived: %v %v", old, err)
	}

	// DeleteOlderThan removes only rows before the cutoff.
	n, err := repo.DeleteOlderThan(ctx, day)
	if err != nil || n != 1 {
		t.Fatalf("delete older: %v %v", n, err)
	}
	remaining, err := repo.GetAllSuccessfulWithImages(ctx)
	if err != nil || len(remaining) != 2 {
		t.Fatalf("remaining: %v %v", remaining, err)
	}
}

// TestSnapshotAnalysisRepo covers the analysis repository queries.
func TestSnapshotAnalysisRepo(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewSnapshotAnalysisRepository(db)

	// GetDetections JOINs cameras and snapshots, so seed both.
	cams := NewCameraRepository(db)
	if err := cams.Add(ctx, &domain.Camera{Name: "Front", Host: "10.0.0.5"}); err != nil {
		t.Fatal(err)
	}
	snaps := NewSnapshotRepository(db)
	snap := &domain.Snapshot{
		CameraID: 1, CapturedAt: domain.NewSQLTime(time.Now()),
		ImagePath: "1.jpg", FileSize: 5, Status: "success",
	}
	if err := snaps.Add(ctx, snap); err != nil {
		t.Fatal(err)
	}

	objJSON := `[{"class_name":"person","confidence":0.9,"bbox":[1,2,3,4]}]`
	a := &domain.SnapshotAnalysis{
		SnapshotID: snap.ID, ModelName: "yolov8n", Status: "completed",
		ObjectsJSON: &objJSON, PersonCount: 1, ReviewRequired: true,
		AnalyzedAt: domain.NullSQLTime{SQLTime: domain.NewSQLTime(time.Now()), Valid: true},
	}
	if err := repo.Add(ctx, a); err != nil {
		t.Fatal(err)
	}
	// Unique constraint: re-adding the same model for one snapshot fails.
	if err := repo.Add(ctx, a); err == nil {
		t.Fatal("expected unique violation")
	}

	got, err := repo.GetByID(ctx, a.ID)
	if err != nil || got.ModelName != "yolov8n" {
		t.Fatalf("get by id: %v %v", got, err)
	}
	bySnap, err := repo.GetBySnapshot(ctx, snap.ID)
	if err != nil || len(bySnap) != 1 {
		t.Fatalf("by snapshot: %v %v", bySnap, err)
	}
	byModel, err := repo.GetBySnapshot(ctx, snap.ID, "other-model")
	if err != nil || len(byModel) != 0 {
		t.Fatalf("by model: %v %v", byModel, err)
	}

	pending, err := repo.GetPendingReviews(ctx, 5)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending: %v %v", pending, err)
	}
	count, err := repo.CountPendingReviews(ctx)
	if err != nil || count != 1 {
		t.Fatalf("count: %v %v", count, err)
	}

	reason := "review me"
	if err := repo.UpdateReview(ctx, a.ID, false, &reason); err != nil {
		t.Fatal(err)
	}
	after, err := repo.GetByID(ctx, a.ID)
	if err != nil || after.ReviewRequired || after.ReviewReason == nil {
		t.Fatalf("after update: %+v %v", after, err)
	}
	// GetByCameraAndDate with no ids returns empty.
	none, err := repo.GetByCameraAndDate(ctx, nil)
	if err != nil || none != nil {
		t.Fatalf("empty ids: %v %v", none, err)
	}

	// GetDetections joins snapshots/cameras.
	rows, err := repo.GetDetections(ctx, nil, nil, 1, "person", 10, 0)
	if err != nil || len(rows) != 1 || rows[0].CameraName == "" {
		t.Fatalf("detections: %v %v", rows, err)
	}

	// DeleteBySnapshotIDs.
	n, err := repo.DeleteBySnapshotIDs(ctx, []int64{snap.ID})
	if err != nil || n != 1 {
		t.Fatalf("delete: %v %v", n, err)
	}
	if n, err := repo.DeleteBySnapshotIDs(ctx, nil); err != nil || n != 0 {
		t.Fatalf("empty delete: %v %v", n, err)
	}
}

// TestAnalysisJobRepoFailure covers the failure state transitions.
func TestAnalysisJobRepoFailure(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewAnalysisJobRepository(db)
	job := &domain.AnalysisJob{SnapshotID: 1, JobType: "yolo_detection", Status: "pending", Priority: 3}
	if err := repo.Add(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkStarted(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkFailed(ctx, job.ID, "detection blew up"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" || got.ErrorMessage == nil || *got.ErrorMessage != "detection blew up" {
		t.Fatalf("unexpected job: %+v", got)
	}
	// Failed jobs must not come back as pending.
	pending, err := repo.GetPending(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending: %v %v", pending, err)
	}
}

// TestRepoNotFoundMapping verifies missing rows surface ErrNotFound.
func TestRepoNotFoundMapping(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	cams := NewCameraRepository(db)
	if _, err := cams.GetByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("camera get: %v", err)
	}
	snaps := NewSnapshotRepository(db)
	if _, err := snaps.GetByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("snapshot get: %v", err)
	}
}

// TestJobRepoDeleteBySnapshotIDs verifies bulk job deletion.
func TestJobRepoDeleteBySnapshotIDs(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewAnalysisJobRepository(db)
	for i := 0; i < 3; i++ {
		if err := repo.Add(ctx, &domain.AnalysisJob{SnapshotID: int64(i + 1), JobType: "yolo_detection"}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := repo.DeleteBySnapshotIDs(ctx, []int64{1, 2})
	if err != nil || n != 2 {
		t.Fatalf("delete: %v %v", n, err)
	}
	if n, err := repo.DeleteBySnapshotIDs(ctx, nil); err != nil || n != 0 {
		t.Fatalf("empty delete: %v %v", n, err)
	}
}

// TestCameraRepoGetAllSorted verifies GetAll ordering by name.
func TestCameraRepoGetAllSorted(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewCameraRepository(db)
	for _, name := range []string{"B", "A", "C"} {
		if err := repo.Add(ctx, &domain.Camera{Name: name, Host: "10.0.0.1"}); err != nil {
			t.Fatal(err)
		}
	}
	cams, err := repo.GetAll(ctx)
	if err != nil || len(cams) != 3 {
		t.Fatalf("get all: %v %v", cams, err)
	}
	if cams[0].Name != "A" || cams[1].Name != "B" || cams[2].Name != "C" {
		t.Fatalf("order = %v", cams)
	}
}
