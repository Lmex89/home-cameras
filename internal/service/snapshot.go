// Package service contains the application services that orchestrate
// repositories and infrastructure adapters. Each service is constructed
// with injected dependencies (dependency inversion).
package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/infrastructure/onvif"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// SnapshotService performs multi-protocol snapshot capture and builds
// reports/dashboards from persisted snapshots.
//
// Capture follows the legacy three-tier fallback chain: direct snapshot
// URL -> ONVIF GetSnapshotUri -> RTSP+ffmpeg frame grab. Network and
// filesystem work happen outside the database transaction (mirroring
// the Python rule of keeping transaction scopes small).
type SnapshotService struct {
	cfg    config.Config
	db     *sqlx.DB
	snaps  *repository.SnapshotRepository
	cams   *repository.CameraRepository
	onvif  *onvif.Client
	client *http.Client
}

// NewSnapshotService builds the snapshot service with injected deps.
//
// Args:
//
//	cfg: Application configuration (paths, timeouts).
//	db: The shared database pool (used to open transactions).
//	snaps: Snapshot repository for reading/writing snapshot rows.
//	cams: Camera repository for lookups (force capture, dashboards).
//	onvifClient: ONVIF adapter used by the ONVIF/RTSP strategies.
//
// Returns:
//
//	A SnapshotService ready to capture.
func NewSnapshotService(cfg config.Config, db *sqlx.DB, snaps *repository.SnapshotRepository, cams *repository.CameraRepository, onvifClient *onvif.Client) *SnapshotService {
	return &SnapshotService{
		cfg:   cfg,
		db:    db,
		snaps: snaps,
		cams:  cams,
		onvif: onvifClient,
		// TLS verification is disabled because many cameras ship
		// self-signed certificates (parity with the legacy httpx
		// verify=False calls).
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// Capture runs the three-tier capture strategy for a camera and
// persists a snapshot record (success or error) atomically.
//
// Args:
//
//	ctx: Request context (timeouts propagate to HTTP and ffmpeg).
//	cam: The camera to capture from.
//
// Returns:
//
//	The persisted Snapshot. Status is "success" or "error".
func (s *SnapshotService) Capture(ctx context.Context, cam domain.Camera) (*domain.Snapshot, error) {
	logger := log.With().Str("camera", cam.Name).Str("host", cam.Host).Logger()
	var lastErr string

	// Strategy 1: direct snapshot URL (cheapest, no SOAP involved).
	if cam.SnapshotURL != nil && *cam.SnapshotURL != "" {
		logger.Info().Str("url", *cam.SnapshotURL).Msg("trying direct snapshot URL")
		relPath, capturedAt, data, err := s.fetchAndSave(ctx, cam, *cam.SnapshotURL, cam.Username, cam.Password)
		if err == nil {
			snap, perr := s.persist(ctx, cam, relPath, capturedAt, int64(len(data)), "success", nil)
			if perr != nil {
				return nil, perr
			}
			logger.Info().Str("path", relPath).Msg("direct URL snapshot saved")
			return snap, nil
		}
		lastErr = err.Error()
		logger.Warn().Str("error", lastErr).Msg("direct URL failed")
	}

	// Strategy 2: ONVIF GetSnapshotUri + HTTP fetch.
	logger.Info().Msg("trying ONVIF GetSnapshotUri")
	if uri, err := s.onvif.GetSnapshotURI(cam.Host, cam.Port, cam.Username, cam.Password, deref(cam.ProfileToken)); err == nil {
		authURI := s.onvif.BuildAuthURL(uri, cam.Username, cam.Password)
		relPath, capturedAt, data, ferr := s.fetchAndSave(ctx, cam, authURI, "", "")
		if ferr == nil {
			snap, perr := s.persist(ctx, cam, relPath, capturedAt, int64(len(data)), "success", nil)
			if perr != nil {
				return nil, perr
			}
			logger.Info().Str("path", relPath).Msg("ONVIF snapshot saved")
			return snap, nil
		}
		lastErr = ferr.Error()
		logger.Warn().Str("error", lastErr).Msg("ONVIF snapshot fetch failed")
	} else {
		lastErr = err.Error()
		logger.Warn().Str("error", lastErr).Msg("ONVIF snapshot URI resolution failed")
	}

	// Strategy 3: RTSP + ffmpeg frame grab.
	logger.Info().Msg("trying RTSP+ffmpeg")
	if data, err := s.captureRTSP(ctx, cam); err == nil {
		relPath, capturedAt := s.saveImage(cam, data)
		snap, perr := s.persist(ctx, cam, relPath, capturedAt, int64(len(data)), "success", nil)
		if perr != nil {
			return nil, perr
		}
		logger.Info().Str("path", relPath).Msg("RTSP snapshot saved")
		return snap, nil
	} else {
		lastErr = err.Error()
		logger.Warn().Str("error", lastErr).Msg("RTSP capture failed")
	}

	logger.Error().Str("error", lastErr).Msg("all capture methods failed")
	return s.persist(ctx, cam, "", time.Now(), 0, "error", &lastErr)
}

// persist inserts the snapshot row inside a transaction, then enqueues
// ML analysis (best effort) when the capture succeeded.
//
// Args:
//
//	ctx: Request context.
//	cam: Camera the snapshot belongs to.
//	imagePath: Relative path under the snapshots dir ("" for errors).
//	capturedAt: Wall-clock capture time.
//	size: File size in bytes.
//	status: "success" or "error".
//	errMsg: Optional error description (only for error records).
//
// Returns:
//
//	The persisted Snapshot.
func (s *SnapshotService) persist(ctx context.Context, cam domain.Camera, imagePath string, capturedAt time.Time, size int64, status string, errMsg *string) (*domain.Snapshot, error) {
	snap := &domain.Snapshot{
		CameraID:     cam.ID,
		CapturedAt:   domain.NewSQLTime(capturedAt),
		ImagePath:    imagePath,
		FileSize:     size,
		Status:       status,
		ErrorMessage: errMsg,
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.snaps.Add(ctx, snap); err != nil {
		log.Error().Err(err).Int64("camera_id", cam.ID).Msg("failed to persist snapshot")
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if status == "success" && s.cfg.AnalysisEnabled {
		if err := s.enqueueAnalysis(ctx, snap.ID); err != nil {
			log.Warn().Err(err).Int64("snapshot_id", snap.ID).Msg("analysis enqueue failed")
		}
	}
	return snap, nil
}

// enqueueAnalysis creates a pending YOLO analysis job for a snapshot.
//
// Args:
//
//	ctx: Request context.
//	snapshotID: The snapshot to analyse.
//
// Returns:
//
//	Any persistence error.
func (s *SnapshotService) enqueueAnalysis(ctx context.Context, snapshotID int64) error {
	repo := repository.NewAnalysisJobRepository(s.db)
	job := &domain.AnalysisJob{
		SnapshotID: snapshotID,
		JobType:    "yolo_detection",
		Status:     "pending",
	}
	return repo.Add(ctx, job)
}

// saveImage writes raw JPEG bytes to
// data/snapshots/{camera_id}/{YYYY}/{MM}/{DD}/{HHMMSS}.jpg and returns
// the path relative to the snapshots directory (as stored in the DB).
//
// Args:
//
//	cam: Camera the image belongs to.
//	data: Raw JPEG bytes.
//
// Returns:
//
//	The relative image path and the capture timestamp.
func (s *SnapshotService) saveImage(cam domain.Camera, data []byte) (string, time.Time) {
	now := time.Now()
	relDir := filepath.Join(fmt.Sprintf("%d", cam.ID), now.Format("2006/01/02"))
	fullDir := filepath.Join(s.cfg.SnapshotsDir(), relDir)
	if err := os.MkdirAll(fullDir, 0o755); err != nil {
		log.Error().Err(err).Str("dir", fullDir).Msg("failed to create snapshot dir")
		return "", now
	}
	relPath := filepath.Join(relDir, now.Format("150405")+".jpg")
	fullPath := filepath.Join(s.cfg.SnapshotsDir(), relPath)
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		log.Error().Err(err).Str("path", fullPath).Msg("failed to write snapshot file")
		return "", now
	}
	return relPath, now
}

// fetchAndSave GETs a URL (with optional basic auth) and stores the
// response body as a snapshot file. Parity with the legacy httpx
// calls: 30s timeout and TLS verification disabled.
//
// Args:
//
//	ctx: Request context.
//	cam: Camera the image belongs to.
//	url: The snapshot URL to fetch.
//	user, pass: Optional basic-auth credentials.
//
// Returns:
//
//	The relative image path, capture time, and raw bytes.
func (s *SnapshotService) fetchAndSave(ctx context.Context, cam domain.Camera, url, user, pass string) (string, time.Time, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", time.Time{}, nil, err
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", time.Time{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, nil, err
	}
	relPath, capturedAt := s.saveImage(cam, data)
	return relPath, capturedAt, data, nil
}

// captureRTSP grabs a single JPEG frame from the camera's RTSP stream
// using ffmpeg. Tries ONVIF stream resolution first, then falls back
// to direct RTSP URL construction for cameras that don't support
// ONVIF GetStreamUri (e.g., Tapo).
//
// Args:
//
//	ctx: Request context.
//	cam: Camera whose RTSP stream will be captured.
//
// Returns:
//
//	The JPEG bytes captured from the stream.
//
// Raises:
//
//	Error: When stream resolution or ffmpeg fails (timeout 30s).
func (s *SnapshotService) captureRTSP(ctx context.Context, cam domain.Camera) ([]byte, error) {
	var streamURI string
	var err error

	if cam.ProfileToken != nil && *cam.ProfileToken != "" {
		if streamURI, err = s.onvif.GetStreamURI(cam.Host, cam.Port, cam.Username, cam.Password, *cam.ProfileToken); err != nil {
			log.Debug().Err(err).Str("camera", cam.Name).Msg("ONVIF GetStreamURI failed, trying direct RTSP")
		}
	} else if streamURI, _, err = s.onvif.GetBestStreamURI(cam.Host, cam.Port, cam.Username, cam.Password); err != nil {
		log.Debug().Err(err).Str("camera", cam.Name).Msg("ONVIF GetBestStreamURI failed, trying direct RTSP")
	}

	// Fall back to direct RTSP URL construction for cameras like Tapo
	// that don't support ONVIF GetStreamUri.
	var directFallback bool
	if streamURI == "" || err != nil {
		streamURI = fmt.Sprintf("rtsp://%s:%s@%s:554/stream1", cam.Username, cam.Password, cam.Host)
		directFallback = true
		log.Debug().Str("camera", cam.Name).Str("uri", streamURI).Msg("using direct RTSP URL")
	}

	if streamURI == "" {
		return nil, errors.New("no RTSP stream found")
	}

	// Only add auth if not already embedded in direct fallback URL
	authURI := streamURI
	if !directFallback {
		authURI = s.onvif.BuildAuthURL(streamURI, cam.Username, cam.Password)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cctx, "ffmpeg",
		"-rtsp_transport", "tcp",
		"-stimeout", "10000000",
		"-i", authURI,
		"-vframes", "1",
		"-q:v", "1",
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"-",
	)
	stderr := new(strings.Builder)
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = msg[len(msg)-300:]
		}
		if cctx.Err() != nil {
			return nil, errors.New("ffmpeg timed out after 30s")
		}
		log.Debug().Str("camera", cam.Name).Str("stderr", msg).Msg("ffmpeg stderr")
		return nil, fmt.Errorf("ffmpeg failed: %s", msg)
	}
	if len(out) == 0 {
		return nil, errors.New("ffmpeg returned no data")
	}
	return out, nil
}

// ForceCapture triggers an immediate out-of-schedule capture.
//
// Args:
//
//	ctx: Request context.
//	cameraID: Primary key of the camera to capture.
//
// Returns:
//
//	The captured Snapshot (mirrors the scheduled capture flow).
func (s *SnapshotService) ForceCapture(ctx context.Context, cameraID int64) (*domain.Snapshot, error) {
	cam, err := s.cams.GetByID(ctx, cameraID)
	if err != nil {
		log.Warn().Err(err).Int64("camera_id", cameraID).Msg("force capture: camera not found")
		return nil, err
	}
	log.Info().Int64("camera_id", cameraID).Msg("force capture triggered")
	return s.Capture(ctx, *cam)
}

// GetDashboardData builds the camera list enriched with each camera's
// latest successful snapshot and total snapshot count.
//
// Args:
//
//	ctx: Request context.
//
// Returns:
//
//	Dashboard items (may be empty).
func (s *SnapshotService) GetDashboardData(ctx context.Context) ([]domain.CameraWithLastSnapshot, error) {
	cams, err := s.cams.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(cams))
	for _, c := range cams {
		ids = append(ids, c.ID)
	}
	lasts, err := s.snaps.GetLastForAllCameras(ctx, ids)
	if err != nil {
		return nil, err
	}
	all, err := s.snaps.GetAllSuccessfulWithImages(ctx)
	if err != nil {
		return nil, err
	}
	totalByCam := map[int64]int{}
	for _, sn := range all {
		totalByCam[sn.CameraID]++
	}
	out := make([]domain.CameraWithLastSnapshot, 0, len(cams))
	for _, c := range cams {
		out = append(out, domain.CameraWithLastSnapshot{
			Camera:         c,
			LastSnapshot:   lasts[c.ID],
			TotalSnapshots: totalByCam[c.ID],
		})
	}
	return out, nil
}

// GetCameraSnapshots lists a camera's snapshots captured on a day.
//
// Args:
//
//	ctx: Request context.
//	cameraID: The camera to query.
//	day: The calendar day (project timezone).
//
// Returns:
//
//	Snapshots ordered by captured_at (may be empty).
func (s *SnapshotService) GetCameraSnapshots(ctx context.Context, cameraID int64, day time.Time) ([]domain.Snapshot, error) {
	return s.snaps.GetByCameraAndDate(ctx, cameraID, day)
}

// GetDailyReport groups all snapshots of a day by camera, attaching ML
// analysis data. Mirrors the /api/report/{date} endpoint behavior.
//
// Args:
//
//	ctx: Request context.
//	day: The calendar day to report on.
//
// Returns:
//
//	The daily report with per-camera snapshot lists.
func (s *SnapshotService) GetDailyReport(ctx context.Context, day time.Time) (*domain.DailyReport, error) {
	all, err := s.snaps.GetByDate(ctx, day)
	if err != nil {
		return nil, err
	}
	analysesRepo := repository.NewSnapshotAnalysisRepository(s.db)

	grouped := map[int64][]domain.Snapshot{}
	for _, sn := range all {
		grouped[sn.CameraID] = append(grouped[sn.CameraID], sn)
	}

	report := &domain.DailyReport{Date: day.Format("2006-01-02")}
	keys := make([]int64, 0, len(grouped))
	for id := range grouped {
		keys = append(keys, id)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	for _, camID := range keys {
		snapList := grouped[camID]
		snapshotIDs := make([]int64, 0, len(snapList))
		for _, sn := range snapList {
			snapshotIDs = append(snapshotIDs, sn.ID)
		}
		analyses, err := analysesRepo.GetByCameraAndDate(ctx, snapshotIDs)
		if err != nil {
			return nil, err
		}
		analysisBySnap := map[int64]*domain.SnapshotAnalysis{}
		for i := range analyses {
			analysisBySnap[analyses[i].SnapshotID] = &analyses[i]
		}
		items := make([]domain.SnapshotWithAnalysis, 0, len(snapList))
		for _, sn := range snapList {
			items = append(items, domain.SnapshotWithAnalysis{
				Snapshot: sn,
				Analysis: analysisBySnap[sn.ID],
			})
		}
		name := fmt.Sprintf("Camera %d", camID)
		if cam, err := s.cams.GetByID(ctx, camID); err == nil {
			name = cam.Name
		}
		report.Cameras = append(report.Cameras, domain.DailyReportCamera{
			CameraID:       camID,
			CameraName:     name,
			TotalSnapshots: len(items),
			Snapshots:      items,
		})
	}
	log.Debug().Int("snapshots", len(all)).Int("cameras", len(report.Cameras)).Msg("daily report built")
	return report, nil
}

// BuildManifest builds the live dashboard manifest consumed by the
// static SPA (parity with the legacy /data/manifest.json endpoint).
//
// Args:
//
//	ctx: Request context.
//
// Returns:
//
//	The manifest map with cameras, per-camera snapshots by date, and
//	generation timestamp.
func (s *SnapshotService) BuildManifest(ctx context.Context) (map[string]any, error) {
	cams, err := s.cams.GetEnabled(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(cams))
	for _, c := range cams {
		ids = append(ids, c.ID)
	}
	lasts, err := s.snaps.GetLastForAllCameras(ctx, ids)
	if err != nil {
		return nil, err
	}
	all, err := s.snaps.GetAllSuccessfulWithImages(ctx)
	if err != nil {
		return nil, err
	}
	totalByCam := map[int64]int{}
	snapshotsByCamDate := map[string]map[string][]any{}
	for _, sn := range all {
		totalByCam[sn.CameraID]++
		key := fmt.Sprintf("%d", sn.CameraID)
		d := sn.CapturedAt.Format("2006-01-02")
		if snapshotsByCamDate[key] == nil {
			snapshotsByCamDate[key] = map[string][]any{}
		}
		snapshotsByCamDate[key][d] = append(snapshotsByCamDate[key][d], map[string]any{
			"image_path":  sn.ImagePath,
			"captured_at": sn.CapturedAt.Format("2006-01-02T15:04:05"),
			"file_size":   sn.FileSize,
		})
	}
	camerasData := make([]domain.CameraWithLastSnapshot, 0, len(cams))
	for _, c := range cams {
		camerasData = append(camerasData, domain.CameraWithLastSnapshot{
			Camera:         c,
			LastSnapshot:   lasts[c.ID],
			TotalSnapshots: totalByCam[c.ID],
		})
	}
	return map[string]any{
		"generated_at": time.Now().Format("2006-01-02T15:04:05"),
		"cameras":      camerasData,
		"snapshots":    snapshotsByCamDate,
	}, nil
}

// GenerateDailyVideo renders a plain (unannotated) timelapse MP4 from a
// camera's snapshots on a date, optionally filtered to one hour.
//
// Args:
//
//	ctx: Request context.
//	cameraID: The camera to build the video for.
//	day: The date of the snapshots.
//	hour: Optional hour filter (0-23); nil means all day.
//
// Returns:
//
//	The output MP4 path and the temp working directory.
//
// Raises:
//
//	Error: When no snapshots exist or ffmpeg fails.
func (s *SnapshotService) GenerateDailyVideo(ctx context.Context, cameraID int64, day time.Time, hour *int) (string, string, error) {
	snaps, err := s.snaps.GetByCameraAndDate(ctx, cameraID, day)
	if err != nil {
		return "", "", err
	}
	if hour != nil {
		filtered := snaps[:0]
		for _, sn := range snaps {
			if sn.CapturedAt.Hour() == *hour {
				filtered = append(filtered, sn)
			}
		}
		snaps = filtered
	}
	if len(snaps) == 0 {
		return "", "", fmt.Errorf("no snapshots for camera %d on %s", cameraID, day.Format("2006-01-02"))
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].CapturedAt.Before(snaps[j].CapturedAt.Time) })
	ts := NewTimelapseService(s.cfg, s.db, s.snaps)
	return ts.RenderPlain(ctx, snaps, day)
}

// deref safely dereferences a *string for ONVIF call parameters.
//
// Args:
//
//	s: The pointer to dereference.
//
// Returns:
//
//	The string value, or "" when the pointer is nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
