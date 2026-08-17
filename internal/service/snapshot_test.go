package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/infrastructure/onvif"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// testJPEG returns a tiny valid JPEG used as fake camera output.
func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// TestCaptureSuccess drives the full capture pipeline against a local
// HTTP snapshot server: URL fetch -> save -> persist -> enqueue.
func TestCaptureSuccess(t *testing.T) {
	cfg, db := newTestDB(t)
	cfg.AnalysisEnabled = true
	ctx := context.Background()

	jpegData := testJPEG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/snap.jpg" {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(jpegData)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cams := repository.NewCameraRepository(db)
	cam := &domain.Camera{
		Name: "Front", Host: srv.URL, Port: 80,
		IntervalSeconds: 60, Enabled: true,
	}
	if err := cams.Add(ctx, cam); err != nil {
		t.Fatal(err)
	}
	// Direct snapshot URL tier.
	snapshotURL := srv.URL + "/snap.jpg"
	cam.SnapshotURL = &snapshotURL

	svc := NewSnapshotService(cfg, db, repository.NewSnapshotRepository(db), cams, onvif.NewFromConfig(cfg))
	snap, err := svc.Capture(ctx, *cam)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if snap.Status != "success" {
		t.Fatalf("status = %q", snap.Status)
	}
	if snap.FileSize != int64(len(jpegData)) {
		t.Errorf("file size = %d want %d", snap.FileSize, len(jpegData))
	}
	full := filepath.Join(cfg.SnapshotsDir(), snap.ImagePath)
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("saved image missing: %v", err)
	}

	// A yolo analysis job must have been enqueued.
	jobs, err := repository.NewAnalysisJobRepository(db).GetPending(ctx, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("pending jobs = %v err %v", jobs, err)
	}
}

// TestCaptureHTTPError records a failed snapshot when the URL returns
// an error status.
func TestCaptureHTTPError(t *testing.T) {
	cfg, db := newTestDB(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cams := repository.NewCameraRepository(db)
	cam := &domain.Camera{Name: "Front", Host: "unused", Port: 80, IntervalSeconds: 60}
	url := srv.URL + "/snap.jpg"
	cam.SnapshotURL = &url
	if err := cams.Add(ctx, cam); err != nil {
		t.Fatal(err)
	}

	svc := NewSnapshotService(cfg, db, repository.NewSnapshotRepository(db), cams, onvif.NewFromConfig(cfg))
	snap, err := svc.Capture(ctx, *cam)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if snap.Status != "error" || snap.ErrorMessage == nil {
		t.Fatalf("expected failed snapshot: %+v", snap)
	}
}

// TestForceCaptureMissingCamera verifies the on-demand capture guard.
func TestForceCaptureMissingCamera(t *testing.T) {
	cfg, db := newTestDB(t)
	ctx := context.Background()
	svc := NewSnapshotService(cfg, db, repository.NewSnapshotRepository(db),
		repository.NewCameraRepository(db), onvif.NewFromConfig(cfg))
	if _, err := svc.ForceCapture(ctx, 999); err == nil {
		t.Fatal("expected error for missing camera")
	}
}

// TestSnapshotReadModels covers the dashboard, report and manifest
// read paths against seeded data.
func TestSnapshotReadModels(t *testing.T) {
	cfg, db := newTestDB(t)
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	cam := &domain.Camera{Name: "Front", Host: "10.0.0.5", Port: 80, IntervalSeconds: 60, Enabled: true}
	disabled := &domain.Camera{Name: "Off", Host: "10.0.0.6", Port: 80, IntervalSeconds: 60, Enabled: false}
	if err := cams.Add(ctx, cam); err != nil {
		t.Fatal(err)
	}
	if err := cams.Add(ctx, disabled); err != nil {
		t.Fatal(err)
	}

	day := time.Date(2026, 8, 7, 12, 0, 0, 0, time.Local)
	snaps := repository.NewSnapshotRepository(db)
	okSnap := &domain.Snapshot{
		CameraID: cam.ID, CapturedAt: domain.NewSQLTime(day),
		ImagePath: "1/2026/08/07/120000.jpg", FileSize: 10, Status: "success",
	}
	failSnap := &domain.Snapshot{
		CameraID: cam.ID, CapturedAt: domain.NewSQLTime(day.Add(-time.Hour)),
		ImagePath: "", FileSize: 0, Status: "error", ErrorMessage: strPtr("nope"),
	}
	if err := snaps.Add(ctx, okSnap); err != nil {
		t.Fatal(err)
	}
	if err := snaps.Add(ctx, failSnap); err != nil {
		t.Fatal(err)
	}

	svc := NewSnapshotService(cfg, db, snaps, cams, onvif.NewFromConfig(cfg))

	dash, err := svc.GetDashboardData(ctx)
	if err != nil || len(dash) != 2 {
		t.Fatalf("dashboard: %v %v", dash, err)
	}
	for _, d := range dash {
		if d.Camera.ID == cam.ID {
			if d.TotalSnapshots != 1 || d.LastSnapshot == nil {
				t.Fatalf("dashboard row: %+v", d)
			}
		}
	}

	byDate, err := svc.GetCameraSnapshots(ctx, cam.ID, day)
	if err != nil || len(byDate) != 2 {
		t.Fatalf("by date: %v %v", byDate, err)
	}
	foundOK := false
	for _, sn := range byDate {
		if sn.Status == "success" {
			foundOK = true
		}
	}
	if !foundOK {
		t.Fatal("by date must include the success snapshot")
	}

	report, err := svc.GetDailyReport(ctx, day)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	// Both snapshots (success + failed attempt) of that day are reported.
	if len(report.Cameras) != 1 || report.Cameras[0].CameraName != "Front" ||
		report.Cameras[0].TotalSnapshots != 2 {
		t.Fatalf("report: %+v", report)
	}

	manifest, err := svc.BuildManifest(ctx)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if manifest["generated_at"] == nil {
		t.Fatal("manifest missing generated_at")
	}

	// Empty-db report path.
	if _, err := snaps.DeleteOlderThan(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	empty, err := svc.GetDailyReport(ctx, day)
	if err != nil || len(empty.Cameras) != 0 {
		t.Fatalf("empty report: %+v %v", empty, err)
	}
}

// TestCaptureUSBFail verifies that USB camera capture cleanly records
// an error when the device path is missing or invalid.
func TestCaptureUSBFail(t *testing.T) {
	cfg, db := newTestDB(t)
	ctx := context.Background()

	cams := repository.NewCameraRepository(db)
	cam := &domain.Camera{
		Name: "USB Cam", CameraType: "usb",
		IntervalSeconds: 60, Enabled: true,
	}
	if err := cams.Add(ctx, cam); err != nil {
		t.Fatal(err)
	}

	svc := NewSnapshotService(cfg, db, repository.NewSnapshotRepository(db), cams, onvif.NewFromConfig(cfg))

	// Missing device_path
	snap, err := svc.Capture(ctx, *cam)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if snap.Status != "error" {
		t.Fatalf("status = %q want error", snap.Status)
	}

	// Invalid device_path
	invalidDev := "/dev/nonexistent_video99"
	cam.DevicePath = &invalidDev
	snap2, err := svc.Capture(ctx, *cam)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if snap2.Status != "error" {
		t.Fatalf("status = %q want error", snap2.Status)
	}
}
