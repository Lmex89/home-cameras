package main

import (
	"context"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/database"
	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/infrastructure/ml"
	"github.com/Lmex89/home-cameras/internal/infrastructure/onvif"
	"github.com/Lmex89/home-cameras/internal/infrastructure/telegram"
	"github.com/Lmex89/home-cameras/internal/repository"
	"github.com/Lmex89/home-cameras/internal/scheduler"
	"github.com/Lmex89/home-cameras/internal/service"
)

// newJobDB builds a temp config + real database for job tests.
func newJobDB(t *testing.T) (config.Config, *sqlx.DB) {
	t.Helper()
	cfg := config.Config{DataDir: t.TempDir()}
	db, err := database.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return cfg, db
}

// TestSetupTimezone covers the TZ loader and fallback.
func TestSetupTimezone(t *testing.T) {
	loc := setupTimezone("UTC")
	if loc.String() != "UTC" {
		t.Fatalf("loc = %v", loc)
	}
	got := setupTimezone("Not/AZone")
	if got == nil {
		t.Fatal("expected a fallback location")
	}
}

// TestFileSizeBytes covers the size helper.
func TestFileSizeBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fileSizeBytes(path); got != 5 {
		t.Fatalf("size = %d", got)
	}
	if got := fileSizeBytes(filepath.Join(t.TempDir(), "nope")); got != 0 {
		t.Fatalf("missing size = %d", got)
	}
}

// TestSetupLogging verifies the log file is created and returned.
func TestSetupLogging(t *testing.T) {
	cfg, _ := newJobDB(t)
	f := setupLogging(config.Config{Debug: true, DataDir: cfg.DataDir})
	if f == nil {
		t.Fatal("expected log file handle")
	}
	f.Close()
	if _, err := os.Stat(filepath.Join(cfg.LogsDir(), "app_"+time.Now().Format("2006-01-02")+".log")); err != nil {
		t.Fatalf("log file missing: %v", err)
	}
}

// TestCaptureJobSuccess drives the capture callback against a local
// HTTP snapshot server.
func TestCaptureJobSuccess(t *testing.T) {
	cfg, db := newJobDB(t)
	cfg.CaptureTimeoutSeconds = 120
	jpeg := []byte("fake-jpeg")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpeg)
	}))
	defer srv.Close()

	cams := repository.NewCameraRepository(db)
	url := srv.URL + "/snap.jpg"
	cam := &domain.Camera{Name: "Front", Host: srv.URL, Port: 80, IntervalSeconds: 60, Enabled: true}
	cam.SnapshotURL = &url
	if err := cams.Add(context.Background(), cam); err != nil {
		t.Fatal(err)
	}

	snaps := repository.NewSnapshotRepository(db)
	svc := service.NewSnapshotService(cfg, db, snaps, cams, onvif.NewFromConfig(cfg))
	streamSvc := service.NewStreamService(onvif.NewFromConfig(cfg), cfg.StreamFPS)
	captureJob(context.Background(), cam.ID, cfg, db, svc, streamSvc)

	last, err := snaps.GetLastByCamera(context.Background(), cam.ID)
	if err != nil || last == nil {
		t.Fatalf("snapshot not persisted: %v", err)
	}
}

// TestCaptureJobGuards covers the missing/disabled camera paths.
func TestCaptureJobGuards(t *testing.T) {
	cfg, db := newJobDB(t)
	cams := repository.NewCameraRepository(db)
	cam := &domain.Camera{Name: "Off", Host: "10.0.0.5", Port: 80, IntervalSeconds: 60, Enabled: false}
	if err := cams.Add(context.Background(), cam); err != nil {
		t.Fatal(err)
	}
	svc := service.NewSnapshotService(cfg, db, repository.NewSnapshotRepository(db), cams, onvif.NewFromConfig(cfg))
	streamSvc := service.NewStreamService(onvif.NewFromConfig(cfg), cfg.StreamFPS)
	captureJob(context.Background(), cam.ID, cfg, db, svc, streamSvc) // disabled -> no-op
	captureJob(context.Background(), 999, cfg, db, svc, streamSvc)    // missing -> warn
}

// TestAnalysisTick verifies the empty-queue tick returns cleanly.
func TestAnalysisTick(t *testing.T) {
	cfg, db := newJobDB(t)
	svc := service.NewAnalysisService(cfg, db, ml.NewDetector(cfg))
	analysisTick(context.Background(), svc)
}

// TestRetentionJob verifies the daily job runs and resumes the
// scheduler afterwards.
func TestRetentionJob(t *testing.T) {
	cfg, db := newJobDB(t)
	sched := scheduler.New(cfg, scheduler.Options{
		CameraCapture: func(ctx context.Context, cameraID int64) {},
		AnalysisTick:  func(ctx context.Context) {},
		RetentionRun:  func(ctx context.Context) {},
		TimelapseRun:  func(ctx context.Context, day time.Time) {},
		HealthRun:     func(ctx context.Context) {},
	})
	svc := service.NewRetentionService(cfg, db)
	retentionJob(context.Background(), sched, svc)
}

// TestTimelapseJobNoSnapshots verifies the job aborts early when the
// camera has no snapshots (no ffmpeg involved).
func TestTimelapseJobNoSnapshots(t *testing.T) {
	cfg, db := newJobDB(t)
	cfg.TimelapseCameraID = 1
	snaps := repository.NewSnapshotRepository(db)
	tlSvc := service.NewTimelapseService(cfg, db, snaps)
	notifier := telegram.New(config.Config{TelegramEnabled: false})
	timelapseJob(context.Background(), cfg, time.Now(), tlSvc, notifier, nil)
}

// TestTimelapseJobSuccess renders a real annotated MP4 (requires
// ffmpeg on PATH).
func TestTimelapseJobSuccess(t *testing.T) {
	cfg, db := newJobDB(t)
	cfg.TimelapseCameraID = 1
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	cam := &domain.Camera{Name: "Front", Host: "10.0.0.5", Port: 80}
	if err := cams.Add(ctx, cam); err != nil {
		t.Fatal(err)
	}
	day := time.Now().AddDate(0, 0, -1)
	snaps := repository.NewSnapshotRepository(db)
	for i := 0; i < 2; i++ {
		at := day.Add(time.Duration(i) * time.Minute)
		rel := filepath.Join("1", at.Format("2006/01/02"), at.Format("150405")+".jpg")
		full := filepath.Join(cfg.SnapshotsDir(), rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		f, err := os.Create(full)
		if err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(f, img, nil); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
		if err := snaps.Add(ctx, &domain.Snapshot{
			CameraID: cam.ID, CapturedAt: domain.NewSQLTime(at),
			ImagePath: rel, FileSize: 10, Status: "success",
		}); err != nil {
			t.Fatal(err)
		}
	}

	tlSvc := service.NewTimelapseService(cfg, db, snaps)
	notifier := telegram.New(config.Config{TelegramEnabled: false})
	timelapseJob(ctx, cfg, day, tlSvc, notifier, nil)

	persistent := filepath.Join(cfg.VideosDir(),
		"timelapse_annotated_1_"+day.Format("2006-01-02")+".mp4")
	if _, err := os.Stat(persistent); err != nil {
		t.Fatalf("annotated timelapse missing: %v", err)
	}
}

// TestHealthJob covers the stale and healthy paths.
func TestHealthJob(t *testing.T) {
	cfg, db := newJobDB(t)
	cfg.CaptureTimeoutSeconds = 120
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	staleCam := &domain.Camera{Name: "Stale", Host: "10.0.0.5", Port: 80, Enabled: true}
	freshCam := &domain.Camera{Name: "Fresh", Host: "10.0.0.6", Port: 80, Enabled: true}
	if err := cams.Add(ctx, staleCam); err != nil {
		t.Fatal(err)
	}
	if err := cams.Add(ctx, freshCam); err != nil {
		t.Fatal(err)
	}
	snaps := repository.NewSnapshotRepository(db)
	if err := snaps.Add(ctx, &domain.Snapshot{
		CameraID: freshCam.ID, CapturedAt: domain.NewSQLTime(time.Now()),
		ImagePath: "x.jpg", FileSize: 1, Status: "success",
	}); err != nil {
		t.Fatal(err)
	}

	notifier := telegram.New(config.Config{TelegramEnabled: false})
	healthJob(ctx, cfg, db, notifier) // stale -> alarm (disabled telegram)
}
