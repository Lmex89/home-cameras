package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/database"
	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/infrastructure/archive"
	"github.com/Lmex89/home-cameras/internal/infrastructure/ml"
	"github.com/Lmex89/home-cameras/internal/infrastructure/onvif"
	"github.com/Lmex89/home-cameras/internal/infrastructure/telegram"
	"github.com/Lmex89/home-cameras/internal/repository"
	"github.com/Lmex89/home-cameras/internal/scheduler"
	"github.com/Lmex89/home-cameras/internal/service"
)

// newTestServer wires a full server (real DB, stub detector, disabled
// telegram/storage) over a temp data dir.
func newTestServer(t *testing.T) (*Server, config.Config) {
	t.Helper()
	cfg := config.Config{DataDir: t.TempDir()}
	ctx := context.Background()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cams := repository.NewCameraRepository(db)
	snaps := repository.NewSnapshotRepository(db)
	onvifClient := onvif.NewFromConfig(cfg)
	detector := ml.NewDetector(cfg)
	archiveReader := archive.New(cfg)
	notifier := telegram.New(cfg)
	sched := scheduler.New(cfg, scheduler.Options{
		CameraCapture: func(ctx context.Context, cameraID int64) {},
		AnalysisTick:  func(ctx context.Context) {},
		RetentionRun:  func(ctx context.Context) {},
		TimelapseRun:  func(ctx context.Context, day time.Time) {},
		HealthRun:     func(ctx context.Context) {},
	})
	t.Cleanup(sched.Stop)

	cameraSvc := service.NewCameraService(db, cams, onvifClient)
	snapshotSvc := service.NewSnapshotService(cfg, db, snaps, cams, onvifClient)
	analysisSvc := service.NewAnalysisService(cfg, db, detector)
	retentionSvc := service.NewRetentionService(cfg, db)
	timelapseSvc := service.NewTimelapseService(cfg, db, snaps)

	deps := Deps{
		Cfg: cfg, DB: db,
		Cameras: cams, Snaps: snaps,
		Onvif: onvifClient, Archive: archiveReader,
		Notifier: notifier, Storage: nil,
		Sched:     sched,
		CameraSvc: cameraSvc, SnapshotSvc: snapshotSvc,
		AnalysisSvc: analysisSvc, Retention: retentionSvc, Timelapse: timelapseSvc,
	}
	return NewServer(deps), cfg
}

// do performs a request against the server router and returns the recorder.
func do(s *Server, method, path, body string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body == "" {
		rd = strings.NewReader("")
	} else {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)
	return w
}

// seedCamera inserts a camera and returns its id.
func seedCamera(t *testing.T, s *Server, name string) int64 {
	t.Helper()
	w := do(s, http.MethodPost, "/api/cameras", `{
		"name": "`+name+`", "host": "10.0.0.5", "port": 80,
		"interval_seconds": 60, "enabled": true
	}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed camera: %d %s", w.Code, w.Body.String())
	}
	// SQLTime has no UnmarshalJSON, so decode only the id.
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.ID
}

// TestHealthzAndManifest covers the read-only status endpoints.
func TestHealthzAndManifest(t *testing.T) {
	s, _ := newTestServer(t)

	w := do(s, http.MethodGet, "/api/healthz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("healthz: %d", w.Code)
	}
	var hz map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &hz); err != nil {
		t.Fatal(err)
	}
	if hz["status"] != "ok" || hz["database"] != true || hz["storage"] != false {
		t.Fatalf("healthz body: %v", hz)
	}

	w = do(s, http.MethodGet, "/api/data/manifest.json", "")
	if w.Code != http.StatusOK {
		t.Fatalf("manifest: %d", w.Code)
	}
}

// TestCamerasCRUD covers create/list/get/update/delete + validation.
func TestCamerasCRUD(t *testing.T) {
	s, _ := newTestServer(t)

	// Validation failures.
	tests := []struct {
		name string
		body string
	}{
		{"missing name", `{"host": "10.0.0.5", "port": 80}`},
		{"missing host", `{"name": "X", "port": 80}`},
		{"bad port", `{"name": "X", "host": "10.0.0.5", "port": 0}`},
		{"port too big", `{"name": "X", "host": "10.0.0.5", "port": 70000}`},
		{"interval too small", `{"name": "X", "host": "10.0.0.5", "port": 80, "interval_seconds": 5}`},
		{"bad json", `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w := do(s, http.MethodPost, "/api/cameras", tt.body); w.Code != http.StatusBadRequest {
				t.Errorf("status = %d want 400 (%s)", w.Code, w.Body.String())
			}
		})
	}

	// Valid create with default interval.
	w := do(s, http.MethodPost, "/api/cameras", `{
		"name": "Front", "host": "10.0.0.5", "port": 80,
		"interval_seconds": 60, "enabled": true
	}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	// SQLTime has no UnmarshalJSON, so decode only the id.
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	id := resp.ID

	// List.
	w = do(s, http.MethodGet, "/api/cameras", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}

	// Get.
	w = do(s, http.MethodGet, "/api/cameras/"+itoa(int(id)), "")
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d", w.Code)
	}

	// Update.
	w = do(s, http.MethodPut, "/api/cameras/"+itoa(int(id)), `{"name": "Front 2", "enabled": false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	// Update of a missing camera -> 404.
	w = do(s, http.MethodPut, "/api/cameras/9999", `{"name": "X"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("update missing: %d", w.Code)
	}

	// Delete.
	w = do(s, http.MethodDelete, "/api/cameras/"+itoa(int(id)), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	w = do(s, http.MethodDelete, "/api/cameras/"+itoa(int(id)), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("second delete: %d", w.Code)
	}
	w = do(s, http.MethodGet, "/api/cameras/"+itoa(int(id)), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("get deleted: %d", w.Code)
	}
}

// TestSnapshotsAndReport covers snapshot listing and the daily report.
func TestSnapshotsAndReport(t *testing.T) {
	s, cfg := newTestServer(t)
	camID := seedCamera(t, s, "Front")

	// Insert a snapshot row + raw file directly.
	db := s.deps.DB
	snaps := repository.NewSnapshotRepository(db)
	day := time.Date(2026, 8, 7, 12, 0, 0, 0, time.Local)
	rel := filepath.Join("1", "2026", "08", "07", "120000.jpg")
	full := filepath.Join(cfg.SnapshotsDir(), rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("fake-jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := &domain.Snapshot{
		CameraID: camID, CapturedAt: domain.NewSQLTime(day),
		ImagePath: rel, FileSize: 10, Status: "success",
	}
	if err := snaps.Add(context.Background(), snap); err != nil {
		t.Fatal(err)
	}

	// Bad date param -> 400.
	w := do(s, http.MethodGet, "/api/snapshots/"+itoa(int(camID))+"/by-date?snapshot_date=nope", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad date: %d", w.Code)
	}
	// Valid by-date.
	w = do(s, http.MethodGet, "/api/snapshots/"+itoa(int(camID))+"/by-date?snapshot_date=2026-08-07", "")
	if w.Code != http.StatusOK {
		t.Fatalf("by date: %d %s", w.Code, w.Body.String())
	}
	// Snapshot detail.
	w = do(s, http.MethodGet, "/api/snapshots/"+itoa(int(snap.ID)), "")
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot detail: %d", w.Code)
	}
	// Missing snapshot.
	w = do(s, http.MethodGet, "/api/snapshots/9999", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing snapshot: %d", w.Code)
	}
	// Image from raw file.
	w = do(s, http.MethodGet, "/api/snapshots/image/"+itoa(int(snap.ID)), "")
	if w.Code != http.StatusOK {
		t.Fatalf("image: %d", w.Code)
	}
	// Image missing.
	w = do(s, http.MethodGet, "/api/snapshots/image/9999", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing image: %d", w.Code)
	}

	// Archive fallback: snapshot with only an archive reference.
	archRef := "snapshots/1/2026-08-06.zip::120000.jpg"
	archZip := filepath.Join(cfg.ArchivesDir(), "snapshots", "1", "2026-08-06.zip")
	if err := os.MkdirAll(filepath.Dir(archZip), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, archZip, map[string]string{"120000.jpg": "archived-jpeg"})
	archived := &domain.Snapshot{
		CameraID: camID, CapturedAt: domain.NewSQLTime(day.AddDate(0, 0, -1)),
		ImagePath: "gone.jpg", FileSize: 5, Status: "success",
	}
	if err := snaps.Add(context.Background(), archived); err != nil {
		t.Fatal(err)
	}
	// Add does not insert archive_path; set it explicitly.
	if err := snaps.UpdateArchivePath(context.Background(), archived.ID, archRef); err != nil {
		t.Fatal(err)
	}
	w = do(s, http.MethodGet, "/api/snapshots/image/"+itoa(int(archived.ID)), "")
	if w.Code != http.StatusOK {
		t.Fatalf("archived image: %d", w.Code)
	}

	// Report valid + invalid date.
	w = do(s, http.MethodGet, "/api/report/2026-08-07", "")
	if w.Code != http.StatusOK {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	w = do(s, http.MethodGet, "/api/report/garbage", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad report date: %d", w.Code)
	}
	// Report video with no snapshots for that camera -> 400.
	w = do(s, http.MethodGet, "/api/report/2026-01-01/video/"+itoa(int(camID)), "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("report video empty: %d %s", w.Code, w.Body.String())
	}
}

// TestReviewsFlow covers the review endpoints end to end.
func TestReviewsFlow(t *testing.T) {
	s, _ := newTestServer(t)
	camID := seedCamera(t, s, "Front")
	db := s.deps.DB

	// Enqueue and process an analysis job for a snapshot with a file.
	snaps := repository.NewSnapshotRepository(db)
	snap := &domain.Snapshot{
		CameraID: camID, CapturedAt: domain.NewSQLTime(time.Now()),
		ImagePath: "1.jpg", FileSize: 10, Status: "success",
	}
	if err := snaps.Add(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	svc := s.deps.AnalysisSvc
	job, err := svc.Enqueue(context.Background(), snap.ID, "yolo_detection", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProcessNextBatch(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	analyses, err := repository.NewSnapshotAnalysisRepository(db).GetBySnapshot(context.Background(), snap.ID)
	if err != nil || len(analyses) != 1 {
		t.Fatalf("analyses: %v %v", analyses, err)
	}
	aID := analyses[0].ID

	// count = 0 initially.
	w := do(s, http.MethodGet, "/api/reviews/count", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"count":0`) {
		t.Fatalf("count: %d %s", w.Code, w.Body.String())
	}

	// Flag the analysis via the review endpoint.
	w = do(s, http.MethodPost, "/api/reviews/"+itoa(int(aID))+"/review", `{"review_required": true, "review_reason": "spotted"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("review update: %d %s", w.Code, w.Body.String())
	}

	// Pending list + count.
	w = do(s, http.MethodGet, "/api/reviews/pending", "")
	if w.Code != http.StatusOK {
		t.Fatalf("pending: %d", w.Code)
	}
	var pending []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0]["camera_name"] != "Front" {
		t.Fatalf("pending: %+v", pending)
	}
	w = do(s, http.MethodGet, "/api/reviews/count", "")
	if !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatalf("count after flag: %s", w.Body.String())
	}

	// Review not found.
	w = do(s, http.MethodPost, "/api/reviews/999/review", `{"review_required": true}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing analysis: %d", w.Code)
	}
	// Bad id.
	w = do(s, http.MethodPost, "/api/reviews/abc/review", `{"review_required": true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", w.Code)
	}

	// Bulk review.
	w = do(s, http.MethodPost, "/api/reviews/bulk-review", `{"analysis_ids": [`+itoa(int(aID))+`], "review_required": false}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"updated":1`) {
		t.Fatalf("bulk: %d %s", w.Code, w.Body.String())
	}

	// Detections browser + validation.
	w = do(s, http.MethodGet, "/api/reviews/detections?days_back=99", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad days_back: %d", w.Code)
	}
	w = do(s, http.MethodGet, "/api/reviews/detections?limit=99999", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad limit: %d", w.Code)
	}
	w = do(s, http.MethodGet, "/api/reviews/detections", "")
	if w.Code != http.StatusOK {
		t.Fatalf("detections: %d", w.Code)
	}
	_ = job
}

// TestRetentionEndpoints covers manual run and purge over the API.
func TestRetentionEndpoints(t *testing.T) {
	s, _ := newTestServer(t)
	seedCamera(t, s, "Front")

	w := do(s, http.MethodPost, "/api/retention/run", "")
	if w.Code != http.StatusOK {
		t.Fatalf("run: %d %s", w.Code, w.Body.String())
	}
	w = do(s, http.MethodPost, "/api/retention/purge", `{"days": 0}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad purge days: %d", w.Code)
	}
	w = do(s, http.MethodPost, "/api/retention/purge", `{"days": 30}`)
	if w.Code != http.StatusOK {
		t.Fatalf("purge: %d %s", w.Code, w.Body.String())
	}
}

// TestVideoEndpoints covers the video generation error paths (no
// snapshots -> 400, avoiding ffmpeg).
func TestVideoEndpoints(t *testing.T) {
	s, _ := newTestServer(t)
	camID := seedCamera(t, s, "Front")

	w := do(s, http.MethodPost, "/api/videos", `{"camera_id": `+itoa(int(camID))+`, "date": "2026-08-07"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("plain video: %d %s", w.Code, w.Body.String())
	}
	w = do(s, http.MethodPost, "/api/videos/annotated", `{"camera_id": `+itoa(int(camID))+`, "date": "2026-08-07"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("annotated video: %d %s", w.Code, w.Body.String())
	}
	// Missing camera -> 404 before rendering.
	w = do(s, http.MethodPost, "/api/videos/annotated", `{"camera_id": 9999, "date": "2026-08-07"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("annotated missing camera: %d", w.Code)
	}
	// Download of an unknown file -> 404.
	w = do(s, http.MethodGet, "/api/videos/download/nope.mp4", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("download: %d", w.Code)
	}
	// Download from the video archive (raw file absent).
	vzip := filepath.Join(s.deps.Cfg.ArchivesDir(), "videos", "1", "2026-08-07.zip")
	if err := os.MkdirAll(filepath.Dir(vzip), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, vzip, map[string]string{"timelapse_1_2026-08-07.mp4": "archived-mp4"})
	w = do(s, http.MethodGet, "/api/videos/download/timelapse_1_2026-08-07.mp4", "")
	if w.Code != http.StatusOK {
		t.Fatalf("archived download: %d", w.Code)
	}
	// Traversal cannot escape the videos dir (chi splits the encoded
	// slashes, leaving an unresolvable filename).
	w = do(s, http.MethodGet, "/api/videos/download/..%2F..%2Fetc%2Fpasswd", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("traversal: %d", w.Code)
	}
}

// TestForceSnapshotEndpoint drives an on-demand capture through the
// API against a local HTTP snapshot server.
func TestForceSnapshotEndpoint(t *testing.T) {
	s, cfg := newTestServer(t)
	jpeg := []byte("forced-jpeg")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpeg)
	}))
	defer srv.Close()

	// Create the camera with a direct snapshot URL via the DB.
	cams := repository.NewCameraRepository(s.deps.DB)
	url := srv.URL + "/snap.jpg"
	cam := &domain.Camera{Name: "Front", Host: srv.URL, Port: 80, IntervalSeconds: 60, Enabled: true}
	cam.SnapshotURL = &url
	if err := cams.Add(context.Background(), cam); err != nil {
		t.Fatal(err)
	}

	w := do(s, http.MethodPost, "/api/cameras/"+itoa(int(cam.ID))+"/snapshot", "")
	if w.Code != http.StatusOK {
		t.Fatalf("force snapshot: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"success":true`) {
		t.Fatalf("body: %s", w.Body.String())
	}
	full := filepath.Join(cfg.SnapshotsDir(), "1")
	matches, _ := filepath.Glob(filepath.Join(full, "*", "*", "*", "*.jpg"))
	if len(matches) == 0 {
		t.Fatal("no snapshot file written")
	}

	// Missing camera -> 404.
	w = do(s, http.MethodPost, "/api/cameras/9999/snapshot", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing camera: %d", w.Code)
	}
}

// TestCameraTestEndpoint probes ONVIF reachability (dead host fails
// fast and returns a non-reachable result).
func TestCameraTestEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(s, http.MethodPost, "/api/cameras/test", `{
		"name": "X", "host": "127.0.0.1", "port": 1
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("test: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"reachable":false`) {
		t.Fatalf("body: %s", w.Body.String())
	}
	w = do(s, http.MethodPost, "/api/cameras/test", `{`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", w.Code)
	}
}

// seedVideoData inserts a camera, 3 snapshots and matching JPEG files
// for the given day, returning the camera id.
func seedVideoData(t *testing.T, s *Server, day time.Time) int64 {
	t.Helper()
	camID := seedCamera(t, s, "Front")
	db := s.deps.DB
	snaps := repository.NewSnapshotRepository(db)
	for i := 0; i < 3; i++ {
		at := day.Add(time.Duration(i) * time.Minute)
		rel := filepath.Join("1", at.Format("2006/01/02"), at.Format("150405")+".jpg")
		full := filepath.Join(s.deps.Cfg.SnapshotsDir(), rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		// Identical-size JPEGs keep the concat demuxer happy.
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				img.Set(x, y, color.RGBA{R: uint8(i * 50), G: 60, B: 90, A: 255})
			}
		}
		f, err := os.Create(full)
		if err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(f, img, nil); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
		if err := snaps.Add(context.Background(), &domain.Snapshot{
			CameraID: camID, CapturedAt: domain.NewSQLTime(at),
			ImagePath: rel, FileSize: 100, Status: "success",
		}); err != nil {
			t.Fatal(err)
		}
	}
	return camID
}

// TestVideoGenerationSuccess generates a real MP4 through the API
// (requires ffmpeg on PATH).
func TestVideoGenerationSuccess(t *testing.T) {
	s, _ := newTestServer(t)
	day := time.Now().AddDate(0, 0, -1)
	camID := seedVideoData(t, s, day)

	w := do(s, http.MethodPost, "/api/videos", `{
		"camera_id": `+itoa(int(camID))+`, "date": "`+day.Format("2006-01-02")+`"
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create video: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		VideoURL string `json:"video_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.VideoURL == "" {
		t.Fatal("expected video url")
	}
	full := filepath.Join(s.deps.Cfg.VideosDir(), filepath.Base(resp.VideoURL))
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("video file missing: %v", err)
	}

	// The report video endpoint streams the same generation.
	w = do(s, http.MethodGet, "/api/report/"+day.Format("2006-01-02")+"/video/"+itoa(int(camID)), "")
	if w.Code != http.StatusOK {
		t.Fatalf("report video: %d %s", w.Code, w.Body.String())
	}
}

// TestAnnotatedVideoSuccess generates an annotated MP4 (requires
// ffmpeg on PATH).
func TestAnnotatedVideoSuccess(t *testing.T) {
	s, _ := newTestServer(t)
	day := time.Now().AddDate(0, 0, -1)
	camID := seedVideoData(t, s, day)

	// Attach a detection analysis to the first snapshot.
	db := s.deps.DB
	snaps := repository.NewSnapshotRepository(db)
	first, err := snaps.GetLastByCamera(context.Background(), camID)
	if err != nil {
		t.Fatal(err)
	}
	objJSON := `[{"class_name":"person","confidence":0.9,"bbox":[5,5,20,40]}]`
	if err := repository.NewSnapshotAnalysisRepository(db).Add(context.Background(),
		&domain.SnapshotAnalysis{
			SnapshotID: first.ID, ModelName: "yolov8n", Status: "completed",
			ObjectsJSON: &objJSON, PersonCount: 1,
			AnalyzedAt: domain.NullSQLTime{SQLTime: domain.NewSQLTime(time.Now()), Valid: true},
		}); err != nil {
		t.Fatal(err)
	}

	w := do(s, http.MethodPost, "/api/videos/annotated", `{
		"camera_id": `+itoa(int(camID))+`, "date": "`+day.Format("2006-01-02")+`",
		"classes": "person"
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("annotated: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"video_url"`) {
		t.Fatalf("body: %s", w.Body.String())
	}
}

// writeTestZip creates a zip at path with the given entries.
func writeTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}
