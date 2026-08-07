package service

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// writeSnapFile creates a raw snapshot file under snapsDir for camera 1
// at the given capture time, returning the DB-stored relative path.
func writeSnapFile(t *testing.T, snapsDir string, capturedAt time.Time) string {
	t.Helper()
	rel := filepath.Join("1", capturedAt.Format("2006/01/02"), capturedAt.Format("150405")+".jpg")
	full := filepath.Join(snapsDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}

// TestDeleteFilesOlderThan verifies the recursive mtime-based deleter.
func TestDeleteFilesOlderThan(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	fresh := time.Now()

	oldFile := filepath.Join(dir, "a.zip")
	newFile := filepath.Join(dir, "b.zip")
	notZip := filepath.Join(dir, "c.mp4")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{oldFile, newFile, notZip, filepath.Join(sub, "d.zip")} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(oldFile, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(sub, "d.zip"), old, old); err != nil {
		t.Fatal(err)
	}
	_ = newFile
	_ = fresh

	n, err := deleteFilesOlderThan(dir, cutoff, ".zip")
	if err != nil {
		t.Fatalf("deleteFilesOlderThan: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted = %d want 2", n)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("old zip should be gone")
	}
	if _, err := os.Stat(filepath.Join(sub, "d.zip")); !os.IsNotExist(err) {
		t.Fatal("subdir old zip should be gone")
	}
	if _, err := os.Stat(notZip); err != nil {
		t.Fatal("non-zip must survive")
	}

	// Missing dir returns 0, nil.
	if n, err := deleteFilesOlderThan(filepath.Join(dir, "nope"), cutoff, ".zip"); err != nil || n != 0 {
		t.Fatalf("missing dir: %v %v", n, err)
	}
}

// TestAppendToZip verifies creation, append and dedupe semantics.
func TestAppendToZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	src := filepath.Join(dir, "in.jpg")
	if err := os.WriteFile(src, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendToZip(zipPath, src, "1.jpg"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := appendToZip(zipPath, src, "2.jpg"); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Re-adding 1.jpg must dedupe, not double.
	if err := appendToZip(zipPath, src, "1.jpg"); err != nil {
		t.Fatalf("dedupe: %v", err)
	}

	names := zipEntryNames(zipPath)
	if len(names) != 2 || !names["1.jpg"] || !names["2.jpg"] {
		t.Fatalf("entries = %v", names)
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "1.jpg" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			buf := make([]byte, 8)
			for {
				n, err := rc.Read(buf)
				b.Write(buf[:n])
				if err != nil {
					break
				}
			}
			rc.Close()
			if b.String() != "one" {
				t.Fatalf("content = %q", b.String())
			}
			if f.Modified.IsZero() {
				t.Fatal("expected modified time preserved")
			}
		}
	}
}

// TestRetentionRunFullPipeline exercises Run() end to end with files
// and rows at various ages.
func TestRetentionRunFullPipeline(t *testing.T) {
	cfg, db := newTestDB(t)
	cfg.SnapshotZipAfterDays = 7
	cfg.SnapshotRetentionDays = 30
	cfg.VideoRetentionDays = 30
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	cam := &domain.Camera{Name: "Front", Host: "10.0.0.5", Port: 80}
	if err := cams.Add(ctx, cam); err != nil {
		t.Fatal(err)
	}
	snaps := repository.NewSnapshotRepository(db)

	now := time.Now()
	addSnap := func(hoursBack int, status string) *domain.Snapshot {
		t.Helper()
		at := now.Add(-time.Duration(hoursBack) * time.Hour)
		rel := ""
		if status == "success" {
			rel = writeSnapFile(t, cfg.SnapshotsDir(), at)
		}
		s := &domain.Snapshot{
			CameraID: cam.ID, CapturedAt: domain.NewSQLTime(at),
			ImagePath: rel, FileSize: 10, Status: status,
		}
		if err := snaps.Add(ctx, s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	// 10 days old (zip-eligible), 40 days old (delete-eligible), fresh.
	oldSnap := addSnap(10*24, "success")
	_ = addSnap(40*24, "success")
	_ = addSnap(1, "success")

	// Old + fresh videos in VideosDir.
	videosDir := cfg.VideosDir()
	if err := os.MkdirAll(videosDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldVideo := filepath.Join(videosDir, "timelapse_1_2026-06-28.mp4")
	freshVideo := filepath.Join(videosDir, "timelapse_1_2026-08-07.mp4")
	for _, f := range []string{oldVideo, freshVideo} {
		if err := os.WriteFile(f, []byte("mp4"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := now.Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(oldVideo, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	// An orphaned video archive zip (old mtime) to be swept.
	orphanZipDir := filepath.Join(cfg.ArchivesDir(), "videos", "1")
	if err := os.MkdirAll(orphanZipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	orphanZip := filepath.Join(orphanZipDir, "2026-05-01.zip")
	if err := os.WriteFile(orphanZip, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(orphanZip, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	svc := NewRetentionService(cfg, db)
	result, err := svc.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if result["snapshots_zipped"] != 2 {
		t.Errorf("zipped = %d want 2 (10d + 40d)", result["snapshots_zipped"])
	}
	if result["snapshots_deleted"] != 1 {
		t.Errorf("deleted = %d want 1 (40d row)", result["snapshots_deleted"])
	}
	if result["videos_archived"] != 1 {
		t.Errorf("videos archived = %d want 1", result["videos_archived"])
	}
	// The archived zip is fresh; the orphan zip (40d old) is swept.
	if result["videos_deleted"] != 1 {
		t.Errorf("videos deleted = %d want 1 (orphan zip)", result["videos_deleted"])
	}

	// Old snapshot raw file must be gone, archive zip must exist.
	if _, err := os.Stat(filepath.Join(cfg.SnapshotsDir(), oldSnap.ImagePath)); !os.IsNotExist(err) {
		t.Fatal("old raw snapshot must be removed after zipping")
	}
	zipAbs := filepath.Join(cfg.ArchivesDir(), "snapshots", "1", oldSnap.CapturedAt.Format("2006-01-02")+".zip")
	if _, err := os.Stat(zipAbs); err != nil {
		t.Fatalf("archive zip missing: %v", err)
	}
	// Fresh video must survive.
	if _, err := os.Stat(freshVideo); err != nil {
		t.Fatal("fresh video must survive")
	}
	// 40d-old snapshot zip became orphaned after row deletion -> swept.
	if _, err := os.Stat(orphanZip); !os.IsNotExist(err) {
		t.Fatal("orphan zip should be gone")
	}
}

// TestRetentionPurge verifies the destructive purge path.
func TestRetentionPurge(t *testing.T) {
	cfg, db := newTestDB(t)
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	cam := &domain.Camera{Name: "Front", Host: "10.0.0.5", Port: 80}
	if err := cams.Add(ctx, cam); err != nil {
		t.Fatal(err)
	}
	snaps := repository.NewSnapshotRepository(db)
	analyses := repository.NewSnapshotAnalysisRepository(db)
	jobs := repository.NewAnalysisJobRepository(db)

	now := time.Now()
	oldSnap := &domain.Snapshot{
		CameraID:   cam.ID,
		CapturedAt: domain.NewSQLTime(now.Add(-10 * 24 * time.Hour)),
		ImagePath:  writeSnapFile(t, cfg.SnapshotsDir(), now.Add(-10*24*time.Hour)),
		FileSize:   10, Status: "success",
	}
	newSnap := &domain.Snapshot{
		CameraID: cam.ID, CapturedAt: domain.NewSQLTime(now.Add(-time.Hour)),
		ImagePath: writeSnapFile(t, cfg.SnapshotsDir(), now.Add(-time.Hour)),
		FileSize:  10, Status: "success",
	}
	for _, s := range []*domain.Snapshot{oldSnap, newSnap} {
		if err := snaps.Add(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := analyses.Add(ctx, &domain.SnapshotAnalysis{
			SnapshotID: s.ID, ModelName: "yolov8n", Status: "completed",
		}); err != nil {
			t.Fatal(err)
		}
		if err := jobs.Add(ctx, &domain.AnalysisJob{
			SnapshotID: s.ID, JobType: "yolo_detection", Status: "pending",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// An orphaned snapshot archive zip for the sweep.
	orphanDir := filepath.Join(cfg.ArchivesDir(), "snapshots", "1")
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	orphanZip := filepath.Join(orphanDir, "2026-05-01.zip")
	if err := os.WriteFile(orphanZip, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewRetentionService(cfg, db)
	result, err := svc.PurgeOlderThan(ctx, 5)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if result["raw_snapshots_deleted"] != 1 {
		t.Errorf("raw deleted = %d want 1", result["raw_snapshots_deleted"])
	}
	if result["snapshots_deleted"] != 1 {
		t.Errorf("rows deleted = %d want 1", result["snapshots_deleted"])
	}
	if result["snapshot_analyses_deleted"] != 1 || result["analysis_jobs_deleted"] != 1 {
		t.Errorf("child deletes wrong: %+v", result)
	}
	if result["snapshot_archives_deleted"] != 1 {
		t.Errorf("archive deletes = %d want 1", result["snapshot_archives_deleted"])
	}
	// The new snapshot must survive with its raw file.
	got, err := snaps.GetByID(ctx, newSnap.ID)
	if err != nil || got == nil {
		t.Fatalf("new snapshot should survive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.SnapshotsDir(), newSnap.ImagePath)); err != nil {
		t.Fatal("new raw file should survive")
	}
	// Old raw file gone.
	if _, err := os.Stat(filepath.Join(cfg.SnapshotsDir(), oldSnap.ImagePath)); !os.IsNotExist(err) {
		t.Fatal("old raw file should be purged")
	}
}
