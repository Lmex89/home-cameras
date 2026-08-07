package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/infrastructure/ml"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// TestApplyReviewRules covers the review heuristics table-driven.
func TestApplyReviewRules(t *testing.T) {
	svc := &AnalysisService{cfg: config.Config{
		ReviewMaxPersonCount:   5,
		ReviewPersonAfterHour:  22,
		ReviewPersonBeforeHour: 6,
	}}

	afterHours := time.Date(2026, 8, 7, 23, 30, 0, 0, time.Local)
	beforeHours := time.Date(2026, 8, 7, 4, 0, 0, 0, time.Local)
	daytime := time.Date(2026, 8, 7, 12, 0, 0, 0, time.Local)

	tests := []struct {
		name        string
		capturedAt  time.Time
		detections  []domain.Detection
		personCount int
		wantFlag    bool
		wantReason  string
	}{
		{
			name:     "no detections",
			wantFlag: false,
		},
		{
			name:        "high person count flags",
			capturedAt:  daytime,
			personCount: 6,
			wantFlag:    true,
			wantReason:  "high_person_count:6",
		},
		{
			name:        "person after hours flags",
			capturedAt:  afterHours,
			personCount: 1,
			wantFlag:    true,
			wantReason:  "person_after_hours:hour=23",
		},
		{
			name:        "person before hours flags",
			capturedAt:  beforeHours,
			personCount: 2,
			wantFlag:    true,
			wantReason:  "person_after_hours:hour=4",
		},
		{
			name:       "unexpected object flags",
			capturedAt: daytime,
			detections: []domain.Detection{{ClassName: "knife", Confidence: 0.9}},
			wantFlag:   true,
			wantReason: "unexpected_objects:knife",
		},
		{
			name:       "allowed objects do not flag",
			capturedAt: daytime,
			detections: []domain.Detection{{ClassName: "car", Confidence: 0.9}, {ClassName: "dog", Confidence: 0.5}},
			wantFlag:   false,
		},
		{
			name:        "combined reasons joined",
			capturedAt:  afterHours,
			personCount: 6,
			detections:  []domain.Detection{{ClassName: "knife", Confidence: 0.8}, {ClassName: "gun", Confidence: 0.7}},
			wantFlag:    true,
			wantReason:  "high_person_count:6; person_after_hours:hour=23; unexpected_objects:gun, knife",
		},
		{
			name:       "unexpected classes sorted and capped at 5",
			capturedAt: daytime,
			detections: []domain.Detection{
				{ClassName: "zebra"}, {ClassName: "apple"}, {ClassName: "banana"},
				{ClassName: "chair"}, {ClassName: "knife"}, {ClassName: "book"},
				{ClassName: "clock"},
			},
			wantFlag:   true,
			wantReason: "unexpected_objects:apple, banana, book, chair, clock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag, reason := svc.applyReviewRules(tt.capturedAt, tt.detections, tt.personCount)
			if flag != tt.wantFlag {
				t.Errorf("flag = %v want %v", flag, tt.wantFlag)
			}
			if tt.wantFlag {
				if reason == nil || *reason != tt.wantReason {
					t.Errorf("reason = %v want %q", reason, tt.wantReason)
				}
			} else if reason != nil {
				t.Errorf("reason = %v want nil", reason)
			}
		})
	}
}

// TestAnalysisPipeline exercises enqueue -> process -> review flow end
// to end with the stub detector (no model, no detections).
func TestAnalysisPipeline(t *testing.T) {
	cfg, db := newTestDB(t)
	cfg = analysisTestCfg(cfg)
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	cam := &domain.Camera{Name: "Front", Host: "10.0.0.5", Port: 80, IntervalSeconds: 60, Enabled: true}
	if err := cams.Add(ctx, cam); err != nil {
		t.Fatalf("add camera: %v", err)
	}

	// Snapshot whose raw image exists on disk (stub detector reads it).
	imgDir := filepath.Join(cfg.SnapshotsDir(), "1", "2026", "08", "07")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(imgDir, "120000.jpg")
	if err := os.WriteFile(imgPath, []byte("fake-jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	snaps := repository.NewSnapshotRepository(db)
	snap := &domain.Snapshot{
		CameraID:   cam.ID,
		CapturedAt: domain.NewSQLTime(time.Date(2026, 8, 7, 12, 0, 0, 0, time.Local)),
		ImagePath:  "1/2026/08/07/120000.jpg", FileSize: 10, Status: "success",
	}
	if err := snaps.Add(ctx, snap); err != nil {
		t.Fatalf("add snapshot: %v", err)
	}

	svc := NewAnalysisService(cfg, db, ml.NewDetector(cfg))
	job, err := svc.Enqueue(ctx, snap.ID, "yolo_detection", 0)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.ID == 0 {
		t.Fatal("expected job id")
	}

	processed, err := svc.ProcessNextBatch(ctx, 5)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d want 1", processed)
	}

	// Stub detection -> completed analysis, no review flag.
	analyses, err := repository.NewSnapshotAnalysisRepository(db).GetBySnapshot(ctx, snap.ID)
	if err != nil {
		t.Fatalf("get analyses: %v", err)
	}
	if len(analyses) != 1 || analyses[0].Status != "completed" {
		t.Fatalf("unexpected analyses: %+v", analyses)
	}
	if analyses[0].ReviewRequired {
		t.Fatal("stub detections must not flag review")
	}

	// Flag it for review and verify the pending queue surfaces it.
	if _, err := svc.UpdateReview(ctx, analyses[0].ID, true, strPtr("manual flag")); err != nil {
		t.Fatalf("update review: %v", err)
	}
	pending, err := svc.GetPendingReviews(ctx, 10)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d want 1", len(pending))
	}
	if pending[0].CameraName != "Front" || pending[0].ReviewReason == nil || *pending[0].ReviewReason != "manual flag" {
		t.Fatalf("unexpected pending item: %+v", pending[0])
	}
	count, err := svc.CountPendingReviews(ctx)
	if err != nil || count != 1 {
		t.Fatalf("count = %d err %v", count, err)
	}
}

// TestAnalysisImageMissing verifies a snapshot without a raw file
// records a failed analysis instead of erroring.
func TestAnalysisImageMissing(t *testing.T) {
	cfg, db := newTestDB(t)
	cfg = analysisTestCfg(cfg)
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	cam := &domain.Camera{Name: "Front", Host: "10.0.0.5", Port: 80}
	if err := cams.Add(ctx, cam); err != nil {
		t.Fatal(err)
	}
	snaps := repository.NewSnapshotRepository(db)
	snap := &domain.Snapshot{
		CameraID:   cam.ID,
		CapturedAt: domain.NewSQLTime(time.Now()),
		ImagePath:  "1/2026/08/07/120000.jpg",
		Status:     "success",
	}
	if err := snaps.Add(ctx, snap); err != nil {
		t.Fatal(err)
	}

	svc := NewAnalysisService(cfg, db, ml.NewDetector(cfg))
	if _, err := svc.Enqueue(ctx, snap.ID, "yolo_detection", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProcessNextBatch(ctx, 5); err != nil {
		t.Fatalf("process: %v", err)
	}
	analyses, err := repository.NewSnapshotAnalysisRepository(db).GetBySnapshot(ctx, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(analyses) != 1 || analyses[0].Status != "failed" {
		t.Fatalf("unexpected analyses: %+v", analyses)
	}
}

// TestAnalysisUnknownJobType verifies unknown job types fail the job.
func TestAnalysisUnknownJobType(t *testing.T) {
	cfg, db := newTestDB(t)
	cfg = analysisTestCfg(cfg)
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	if err := cams.Add(ctx, &domain.Camera{Name: "Front", Host: "10.0.0.5"}); err != nil {
		t.Fatal(err)
	}
	snaps := repository.NewSnapshotRepository(db)
	snap := &domain.Snapshot{
		CameraID:   1,
		CapturedAt: domain.NewSQLTime(time.Now()),
		ImagePath:  "1.jpg",
		Status:     "success",
	}
	if err := snaps.Add(ctx, snap); err != nil {
		t.Fatal(err)
	}
	jobs := repository.NewAnalysisJobRepository(db)
	if err := jobs.Add(ctx, &domain.AnalysisJob{SnapshotID: snap.ID, JobType: "bogus", Status: "pending"}); err != nil {
		t.Fatal(err)
	}

	svc := NewAnalysisService(cfg, db, ml.NewDetector(cfg))
	processed, err := svc.ProcessNextBatch(ctx, 5)
	if err != nil || processed != 0 {
		t.Fatalf("processed = %d err %v (want 0, nil)", processed, err)
	}
	job, err := jobs.GetByID(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "failed" {
		t.Fatalf("job status = %q want failed", job.Status)
	}
}

// TestAnalysisAnomalyJob verifies the anomaly scoring stub job type.
func TestAnalysisAnomalyJob(t *testing.T) {
	cfg, db := newTestDB(t)
	cfg = analysisTestCfg(cfg)
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	if err := cams.Add(ctx, &domain.Camera{Name: "Front", Host: "10.0.0.5"}); err != nil {
		t.Fatal(err)
	}
	snaps := repository.NewSnapshotRepository(db)
	snap := &domain.Snapshot{
		CameraID: 1, CapturedAt: domain.NewSQLTime(time.Now()),
		ImagePath: "1.jpg", Status: "success",
	}
	if err := snaps.Add(ctx, snap); err != nil {
		t.Fatal(err)
	}

	svc := NewAnalysisService(cfg, db, ml.NewDetector(cfg))
	if _, err := svc.Enqueue(ctx, snap.ID, "anomaly_scoring", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProcessNextBatch(ctx, 5); err != nil {
		t.Fatalf("process: %v", err)
	}
	analyses, err := repository.NewSnapshotAnalysisRepository(db).GetBySnapshot(ctx, snap.ID, "anomalib")
	if err != nil || len(analyses) != 1 {
		t.Fatalf("analyses: %v %v", analyses, err)
	}
	if analyses[0].Status != "completed" {
		t.Fatalf("status = %q", analyses[0].Status)
	}
}

// TestUpdateReviewNotFound verifies a missing analysis returns nil.
func TestUpdateReviewNotFound(t *testing.T) {
	cfg, db := newTestDB(t)
	svc := NewAnalysisService(analysisTestCfg(cfg), db, ml.NewDetector(cfg))
	got, err := svc.UpdateReview(context.Background(), 999, true, nil)
	if err != nil || got != nil {
		t.Fatalf("got %v err %v (want nil, nil)", got, err)
	}
}

// TestGetDetections verifies the detection browser query filters.
func TestGetDetections(t *testing.T) {
	cfg, db := newTestDB(t)
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	if err := cams.Add(ctx, &domain.Camera{Name: "Front", Host: "10.0.0.5"}); err != nil {
		t.Fatal(err)
	}
	snaps := repository.NewSnapshotRepository(db)
	day := time.Now()
	snap := &domain.Snapshot{
		CameraID:   1,
		CapturedAt: domain.NewSQLTime(day),
		ImagePath:  "1.jpg", Status: "success",
	}
	if err := snaps.Add(ctx, snap); err != nil {
		t.Fatal(err)
	}
	analyses := repository.NewSnapshotAnalysisRepository(db)
	objJSON := `[{"class_name":"person","confidence":0.9,"bbox":[1,2,3,4]}]`
	if err := analyses.Add(ctx, &domain.SnapshotAnalysis{
		SnapshotID:     snap.ID,
		ModelName:      "yolov8n",
		Status:         "completed",
		ObjectsJSON:    &objJSON,
		PersonCount:    1,
		ReviewRequired: true,
		AnalyzedAt:     domain.NullSQLTime{SQLTime: domain.NewSQLTime(time.Now()), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewAnalysisService(cfg, db, ml.NewDetector(cfg))
	rows, err := svc.GetDetections(ctx, 7, 0, "person", 10, 0, "")
	if err != nil {
		t.Fatalf("detections: %v", err)
	}
	if len(rows) != 1 || rows[0].CameraName != "Front" {
		t.Fatalf("rows = %+v", rows)
	}
	if _, err := svc.GetDetections(ctx, 0, 1, "", 10, 0, "2026-08-07"); err != nil {
		t.Fatalf("date-from detections: %v", err)
	}
	if _, err := svc.GetDetections(ctx, 0, 0, "", 10, 0, ""); err != nil {
		t.Fatalf("today detections: %v", err)
	}
}
