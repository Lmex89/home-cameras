// Command server runs the camera monitor web service. It wires config,
// logging, database, services, scheduler and the HTTP server, then
// blocks until a termination signal arrives.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/api"
	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/database"
	"github.com/Lmex89/home-cameras/internal/infrastructure/archive"
	"github.com/Lmex89/home-cameras/internal/infrastructure/ml"
	"github.com/Lmex89/home-cameras/internal/infrastructure/onvif"
	"github.com/Lmex89/home-cameras/internal/infrastructure/storage"
	"github.com/Lmex89/home-cameras/internal/infrastructure/telegram"
	"github.com/Lmex89/home-cameras/internal/repository"
	"github.com/Lmex89/home-cameras/internal/scheduler"
	"github.com/Lmex89/home-cameras/internal/seed"
	"github.com/Lmex89/home-cameras/internal/service"
)

// setupLogging configures zerolog: human-readable console output at the
// configured level plus a daily rotating file under data/logs/ (parity
// with the legacy loguru setup).
//
// Args:
//
//	cfg: Application configuration.
//
// Returns:
//
//	The rotated log file handle (closed on shutdown).
func setupLogging(cfg config.Config) *os.File {
	level := zerolog.InfoLevel
	if cfg.Debug {
		level = zerolog.DebugLevel
	}
	zerolog.SetGlobalLevel(level)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "2006-01-02 15:04:05"})

	if err := os.MkdirAll(cfg.LogsDir(), 0o755); err != nil {
		log.Warn().Err(err).Msg("cannot create logs dir")
		return nil
	}
	logPath := filepath.Join(cfg.LogsDir(), "app_"+time.Now().Format("2006-01-02")+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Warn().Err(err).Msg("cannot open log file")
		return nil
	}
	multi := zerolog.MultiLevelWriter(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "2006-01-02 15:04:05"}, f)
	log.Logger = log.Output(multi)
	return f
}

// setupTimezone applies the configured TZ to the process so time.Now()
// and all time parsing use the project timezone (parity with the legacy
// os.environ["TZ"] + tzset()).
//
// Args:
//
//	tz: IANA timezone name.
//
// Returns:
//
//	The loaded location.
func setupTimezone(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Warn().Err(err).Str("tz", tz).Msg("invalid timezone, using local")
		return time.Local
	}
	time.Local = loc
	os.Setenv("TZ", tz)
	return loc
}

// captureJob is the scheduler callback that captures one camera.
//
// Args:
//
//	ctx: Job context.
//	cameraID: The camera to capture.
//	cfg: Application config (capture timeout).
//	db: Database pool.
//	snapshotSvc: The snapshot service.
//	streamSvc: The stream service (to skip USB cameras that are streaming).
func captureJob(ctx context.Context, cameraID int64, cfg config.Config, db *sqlx.DB, snapshotSvc *service.SnapshotService, streamSvc *service.StreamService) {
	repo := repository.NewCameraRepository(db)
	cam, err := repo.GetByID(ctx, cameraID)
	if err != nil {
		log.Warn().Err(err).Int64("camera_id", cameraID).Msg("capture job: camera not found")
		return
	}
	if !cam.Enabled {
		return
	}
	if cam.CameraType == "usb" && streamSvc.IsStreaming(cameraID) {
		log.Debug().Int64("camera_id", cameraID).Msg("capture job: skipped (USB camera is streaming)")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.CaptureTimeoutSeconds)*time.Second)
	defer cancel()
	snap, err := snapshotSvc.Capture(cctx, *cam)
	if err != nil {
		log.Error().Err(err).Int64("camera_id", cameraID).Msg("capture job failed")
		return
	}
	status := "ok"
	if snap.Status != "success" {
		status = "error"
	}
	log.Info().Int64("camera_id", cameraID).Str("status", status).Msg("camera snapshot capture")
}

// analysisTick is the scheduler callback that processes pending jobs.
//
// Args:
//
//	ctx: Job context.
//	analysisSvc: The analysis service.
func analysisTick(ctx context.Context, analysisSvc *service.AnalysisService) {
	processed, err := analysisSvc.ProcessNextBatch(ctx, 50)
	if err != nil {
		log.Error().Err(err).Msg("analysis tick failed")
		return
	}
	if processed > 0 {
		log.Info().Int("processed", processed).Msg("analysis tick processed jobs")
	}
}

// retentionJob is the scheduler callback for daily retention; it pauses
// capture/analysis while running (SQLite single-writer mitigation).
//
// Args:
//
//	ctx: Job context.
//	sched: The scheduler (for pause/resume).
//	retentionSvc: The retention service.
func retentionJob(ctx context.Context, sched *scheduler.Scheduler, retentionSvc *service.RetentionService) {
	sched.PauseAll()
	defer sched.ResumeAll()
	result, err := retentionSvc.Run(ctx)
	if err != nil {
		log.Error().Err(err).Msg("retention job failed")
		return
	}
	log.Info().Interface("result", result).Msg("retention job complete")
}

// timelapseJob generates the annotated timelapse for a given day,
// uploads it to storage, and notifies Telegram.
//
// Args:
//
//	ctx: Job context.
//	cfg: Application config.
//	day: The date to render.
//	timelapseSvc: The timelapse service.
//	notifier: Telegram notifier.
//	storageSvc: Optional S3 uploader.
func timelapseJob(ctx context.Context, cfg config.Config, day time.Time, timelapseSvc *service.TimelapseService, notifier *telegram.Notifier, storageSvc *storage.S3) {
	cameraID := int64(cfg.TimelapseCameraID)
	outputPath, tempDir, err := timelapseSvc.RenderAnnotated(ctx, cameraID, day, nil)
	if err != nil {
		log.Error().Err(err).Msg("timelapse job failed")
		return
	}
	vdir := cfg.VideosDir()
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		log.Warn().Err(err).Str("dir", vdir).Msg("cannot create videos dir")
	}
	persistent := filepath.Join(vdir, fmt.Sprintf("timelapse_annotated_%d_%s.mp4", cameraID, day.Format("2006-01-02")))
	if err := os.Rename(outputPath, persistent); err != nil {
		log.Error().Err(err).Msg("timelapse move failed")
		os.RemoveAll(tempDir)
		return
	}
	os.RemoveAll(tempDir)
	log.Info().Str("path", persistent).Msg("annotated timelapse saved")

	var blazeURL string
	if storageSvc != nil {
		if url, err := storageSvc.Upload(ctx, persistent); err == nil {
			blazeURL = url
		} else {
			log.Warn().Err(err).Str("path", persistent).Msg("timelapse upload to storage failed")
		}
	}
	sizeMB := float64(fileSizeBytes(persistent)) / (1024 * 1024)
	caption := fmt.Sprintf("\U0001f3a5 Camera %d — timelapse %s (annotated, %.1f MB)", cameraID, day.Format("2006-01-02"), sizeMB)
	notifier.SendVideo(ctx, persistent, caption,
		fmt.Sprintf("http://localhost:%d/api/videos/download/%s", cfg.Port, filepath.Base(persistent)),
		blazeURL)
}

// healthJob checks that cameras produced recent snapshots and raises a
// Telegram alarm when any is stale.
//
// Args:
//
//	ctx: Job context.
//	cfg: Application config.
//	db: Database pool.
//	notifier: Telegram notifier.
func healthJob(ctx context.Context, cfg config.Config, db *sqlx.DB, notifier *telegram.Notifier) {
	threshold := time.Duration(cfg.CaptureTimeoutSeconds*2) * time.Second
	cutoff := time.Now().Add(-threshold)
	repo := repository.NewCameraRepository(db)
	snapsRepo := repository.NewSnapshotRepository(db)

	cams, err := repo.GetEnabled(ctx)
	if err != nil {
		log.Error().Err(err).Msg("health check failed")
		return
	}
	var stale []string
	for _, cam := range cams {
		last, err := snapsRepo.GetLastByCamera(ctx, cam.ID)
		if err != nil || last == nil {
			stale = append(stale, fmt.Sprintf("%s (ID %d): last never", cam.Name, cam.ID))
			continue
		}
		if last.CapturedAt.Time.Before(cutoff) {
			age := time.Since(last.CapturedAt.Time).Round(time.Second)
			stale = append(stale, fmt.Sprintf("%s (ID %d): last %s ago", cam.Name, cam.ID, age))
		}
	}
	if len(stale) > 0 {
		msg := fmt.Sprintf("Camera Monitor HEALTH ALARM\nNo recent snapshots (threshold: %s)\nTime: %s\n\n",
			threshold, time.Now().Format("2006-01-02 15:04:05"))
		for _, s := range stale {
			msg += "  - " + s + "\n"
		}
		log.Error().Int("stale", len(stale)).Msg("health check failed")
		if !notifier.SendMessage(ctx, msg) {
			log.Warn().Msg("health alarm failed to send")
		}
		return
	}
	log.Debug().Int("cameras", len(cams)).Msg("health check OK")
}

// fileSizeBytes returns a file size in bytes (0 when unreadable).
//
// Args:
//
//	path: The file to measure.
//
// Returns:
//
//	The size in bytes.
func fileSizeBytes(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	logFile := setupLogging(cfg)
	if logFile != nil {
		defer logFile.Close()
	}
	setupTimezone(cfg.Timezone)

	// Data directories.
	for _, dir := range []string{cfg.DataDir, cfg.SnapshotsDir(), cfg.ArchivesDir(), cfg.VideosDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal().Err(err).Str("dir", dir).Msg("cannot create data dir")
		}
	}

	ctx := context.Background()

	// Database (schema + legacy migrations applied on open).
	db, err := database.Open(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("database init failed")
	}
	defer db.Close()

	// Seed cameras from cameras.yaml (idempotent).
	if _, err := seed.FromYAML(ctx, db, cfg.YAMLPath(), cfg.DefaultIntervalSeconds); err != nil {
		log.Warn().Err(err).Msg("seeding failed")
	}

	// Infrastructure.
	onvifClient := onvif.NewFromConfig(cfg)
	detector := ml.NewDetector(cfg)
	if !detector.Available() {
		if modelPath := ml.ModelPath(cfg); modelPath == "" {
			log.Warn().Str("model_path", cfg.YoloModelPath).
				Msg("object detector unavailable; analysis will use stub mode: no usable model found (gocv reads ONNX, not .pt — run 'make model' to download and export it)")
		} else {
			log.Warn().Str("model_path", modelPath).
				Msg("object detector unavailable; analysis will use stub mode: binary not built with the opencv tag (rebuild with 'make opencv')")
		}
	}
	archiveReader := archive.New(cfg)
	notifier := telegram.New(cfg)
	var storageSvc *storage.S3
	if cfg.StorageEnabled {
		if s3, err := storage.NewFromConfig(cfg); err == nil {
			storageSvc = s3
		} else {
			log.Warn().Err(err).Msg("storage not configured")
		}
	}

	// Repositories.
	camerasRepo := repository.NewCameraRepository(db)
	snapsRepo := repository.NewSnapshotRepository(db)
	analysesRepo := repository.NewSnapshotAnalysisRepository(db)
	jobsRepo := repository.NewAnalysisJobRepository(db)

	// Services.
	cameraSvc := service.NewCameraService(camerasRepo, onvifClient)
	timelapseSvc := service.NewTimelapseService(cfg, db, snapsRepo)
	snapshotSvc := service.NewSnapshotService(cfg, db, snapsRepo, camerasRepo, jobsRepo, analysesRepo, timelapseSvc, onvifClient)
	analysisSvc := service.NewAnalysisService(cfg, db, detector, jobsRepo, snapsRepo, analysesRepo, camerasRepo)
	retentionSvc := service.NewRetentionService(cfg, db, snapsRepo, analysesRepo, jobsRepo)
	streamSvc := service.NewStreamService(onvifClient, cfg.StreamFPS)

	// Scheduler with job callbacks.
	var sched *scheduler.Scheduler
	sched = scheduler.New(cfg, scheduler.Options{
		CameraCapture: func(c context.Context, id int64) { captureJob(c, id, cfg, db, snapshotSvc, streamSvc) },
		AnalysisTick:  func(c context.Context) { analysisTick(c, analysisSvc) },
		RetentionRun:  func(c context.Context) { retentionJob(c, sched, retentionSvc) },
		TimelapseRun: func(c context.Context, day time.Time) {
			timelapseJob(c, cfg, day, timelapseSvc, notifier, storageSvc)
		},
		HealthRun: func(c context.Context) { healthJob(c, cfg, db, notifier) },
	})

	// Schedule enabled cameras, anchoring intervals to their last
	// successful snapshot so the schedule survives restarts.
	cams, err := camerasRepo.GetEnabled(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot load cameras for scheduling")
	}
	for _, cam := range cams {
		anchor := time.Now()
		if last, err := snapsRepo.GetLastByCamera(ctx, cam.ID); err == nil {
			anchor = last.CapturedAt.Time
		}
		sched.ScheduleCamera(cam.ID, cam.IntervalSeconds, anchor)
	}
	sched.Start()

	// HTTP server.
	deps := api.Deps{
		Cfg:         cfg,
		DB:          db,
		Cameras:     camerasRepo,
		Snaps:       snapsRepo,
		Onvif:       onvifClient,
		Archive:     archiveReader,
		Notifier:    notifier,
		Storage:     storageSvc,
		Sched:       sched,
		StreamSvc:   streamSvc,
		CameraSvc:   cameraSvc,
		SnapshotSvc: snapshotSvc,
		AnalysisSvc: analysisSvc,
		Retention:   retentionSvc,
		Timelapse:   timelapseSvc,
	}
	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           api.NewServer(deps).Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		log.Info().Msg("shutting down")
		sched.Stop()
		streamSvc.Shutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("http shutdown failed")
		}
	}()

	log.Info().Str("addr", server.Addr).Str("app", cfg.AppName).Msg("camera monitor started")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal().Err(err).Msg("http server failed")
	}
	log.Info().Msg("camera monitor stopped")
}
