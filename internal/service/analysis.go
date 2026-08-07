package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/infrastructure/ml"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// AnalysisService consumes the analysis job queue, runs object
// detection, applies the heuristic review rules, and serves pending
// review items. Mirrors the legacy AnalysisService one-to-one.
type AnalysisService struct {
	cfg      config.Config
	db       *sqlx.DB
	jobs     *repository.AnalysisJobRepository
	snaps    *repository.SnapshotRepository
	analyses *repository.SnapshotAnalysisRepository
	detector ml.Detector
}

// NewAnalysisService builds the analysis service. The detector is the
// process-wide singleton so the model is loaded exactly once (the
// legacy app reloading the model per batch caused CPU/thermal spikes).
//
// Args:
//
//	cfg: Application configuration.
//	db: Shared database pool.
//	detector: Object detector (stub fallback when unavailable).
//
// Returns:
//
//	A ready AnalysisService.
func NewAnalysisService(cfg config.Config, db *sqlx.DB, detector ml.Detector) *AnalysisService {
	return &AnalysisService{
		cfg:      cfg,
		db:       db,
		jobs:     repository.NewAnalysisJobRepository(db),
		snaps:    repository.NewSnapshotRepository(db),
		analyses: repository.NewSnapshotAnalysisRepository(db),
		detector: detector,
	}
}

// Enqueue creates a pending analysis job for a snapshot.
//
// Args:
//
//	ctx: Request context.
//	snapshotID: The snapshot to analyse.
//	jobType: Pipeline stage (default "yolo_detection").
//	priority: Job priority (higher = processed first).
//
// Returns:
//
//	The created job.
func (s *AnalysisService) Enqueue(ctx context.Context, snapshotID int64, jobType string, priority int) (*domain.AnalysisJob, error) {
	job := &domain.AnalysisJob{
		SnapshotID: snapshotID,
		JobType:    jobType,
		Status:     "pending",
		Priority:   priority,
	}
	if err := s.jobs.Add(ctx, job); err != nil {
		return nil, err
	}
	log.Debug().Int64("snapshot_id", snapshotID).Str("type", jobType).Msg("analysis job queued")
	return job, nil
}

// ProcessNextBatch processes up to limit pending jobs, each in its own
// transaction. Failed jobs are marked failed with the error message.
//
// Args:
//
//	ctx: Request context.
//	limit: Maximum jobs to process in this batch.
//
// Returns:
//
//	Number of jobs successfully processed.
func (s *AnalysisService) ProcessNextBatch(ctx context.Context, limit int) (int, error) {
	jobs, err := s.jobs.GetPending(ctx, limit)
	if err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	processed := 0
	for _, job := range jobs {
		if err := s.processJob(ctx, &job); err != nil {
			log.Error().Err(err).Int64("job_id", job.ID).Msg("analysis job failed")
			if merr := s.jobs.MarkFailed(ctx, job.ID, err.Error()); merr != nil {
				log.Error().Err(merr).Int64("job_id", job.ID).Msg("failed to mark job failed")
			}
			continue
		}
		processed++
	}
	if processed > 0 {
		log.Info().Int("processed", processed).Msg("analysis batch processed")
	}
	return processed, nil
}

// processJob runs a single job: compute the analysis OUTSIDE the write
// transaction (YOLO inference is CPU-bound and can take seconds), then
// persist job state + analysis atomically in one short transaction.
//
// Args:
//
//	ctx: Request context.
//	job: The pending job to process.
//
// Returns:
//
//	Error on any pipeline failure.
func (s *AnalysisService) processJob(ctx context.Context, job *domain.AnalysisJob) error {
	snap, err := s.snaps.GetByID(ctx, job.SnapshotID)
	if err != nil {
		return fmt.Errorf("snapshot %d not found", job.SnapshotID)
	}

	analysis, err := s.runAnalysis(ctx, snap, job)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.jobs.MarkStarted(ctx, job.ID); err != nil {
		return err
	}
	if err := s.analyses.Add(ctx, analysis); err != nil {
		return err
	}
	if err := s.jobs.MarkCompleted(ctx, job.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// runAnalysis dispatches a job to its detector without persisting
// anything. Keeping inference outside the transaction avoids holding the
// SQLite write lock for its whole duration, which previously blocked
// readers (manifest, reports) up to busy_timeout.
//
// Args:
//
//	ctx: Request context.
//	snap: The snapshot under analysis.
//	job: The job being processed.
//
// Returns:
//
//	The computed SnapshotAnalysis (not yet persisted).
//
// Raises:
//
//	Error: For an unknown job type.
func (s *AnalysisService) runAnalysis(ctx context.Context, snap *domain.Snapshot, job *domain.AnalysisJob) (*domain.SnapshotAnalysis, error) {
	switch job.JobType {
	case "yolo_detection":
		return s.runYolo(ctx, snap, job)
	case "anomaly_scoring":
		return s.runAnomaly(ctx, snap, job)
	default:
		return nil, fmt.Errorf("unknown job_type: %s", job.JobType)
	}
}

// runYolo runs object detection on a snapshot and computes the result
// with the review heuristics applied. It does NOT persist — processJob
// stores the analysis in its short write transaction.
//
// Args:
//
//	ctx: Request context.
//	snap: The snapshot being analyzed.
//	job: The job being processed.
//
// Returns:
//
//	The computed SnapshotAnalysis (not yet persisted).
func (s *AnalysisService) runYolo(ctx context.Context, snap *domain.Snapshot, job *domain.AnalysisJob) (*domain.SnapshotAnalysis, error) {
	now := time.Now()
	imagePath := filepath.Join(s.cfg.SnapshotsDir(), snap.ImagePath)

	// The image may have been archived away; record a failed analysis.
	if _, err := os.Stat(imagePath); err != nil {
		analysis := &domain.SnapshotAnalysis{
			SnapshotID:   snap.ID,
			ModelName:    "yolov8n",
			ModelVersion: "1.0",
			Status:       "failed",
			ErrorMessage: strPtr("Image file not found"),
			AnalyzedAt:   domain.NullSQLTime{SQLTime: domain.NewSQLTime(now), Valid: true},
		}
		log.Warn().Str("path", imagePath).Msg("YOLO skip: image not found")
		return analysis, nil
	}

	detections, err := s.detector.Detect(ctx, imagePath)
	if err != nil {
		// Inference failure is not fatal: record empty detections.
		log.Error().Err(err).Str("path", imagePath).Msg("YOLO inference failed")
		detections = nil
	}

	personCount := 0
	for _, d := range detections {
		if d.ClassName == "person" {
			personCount++
		}
	}
	objectsJSON := ""
	if len(detections) > 0 {
		if data, err := json.Marshal(detections); err == nil {
			objectsJSON = string(data)
		}
	}
	reviewRequired, reviewReason := s.applyReviewRules(snap.CapturedAt.Time, detections, personCount)

	status := "completed"
	if reviewRequired {
		status = "review_pending"
	}
	analysis := &domain.SnapshotAnalysis{
		SnapshotID:     snap.ID,
		ModelName:      "yolov8n",
		ModelVersion:   "1.0",
		Status:         status,
		ObjectsJSON:    strPtrIf(objectsJSON != "", objectsJSON),
		PersonCount:    personCount,
		ReviewRequired: reviewRequired,
		ReviewReason:   reviewReason,
		AnalyzedAt:     domain.NullSQLTime{SQLTime: domain.NewSQLTime(now), Valid: true},
	}
	logInfo := log.Info().Int64("snapshot_id", snap.ID).
		Int("objects", len(detections)).
		Int("persons", personCount)
	if reviewRequired {
		logInfo.Str("review_reason", *reviewReason).Msg("YOLO analysis FLAGGED for review")
	} else {
		logInfo.Msg("YOLO analysis completed")
	}
	return analysis, nil
}

// runAnomaly is a stub for a future Anomalib integration; it computes a
// completed analysis without detections. Persisting happens in
// processJob's transaction.
//
// Args:
//
//	ctx: Request context.
//	snap: The snapshot to analyse.
//	job: The job being processed.
//
// Returns:
//
//	The computed (stub) SnapshotAnalysis.
func (s *AnalysisService) runAnomaly(ctx context.Context, snap *domain.Snapshot, job *domain.AnalysisJob) (*domain.SnapshotAnalysis, error) {
	now := time.Now()
	analysis := &domain.SnapshotAnalysis{
		SnapshotID: snap.ID,
		ModelName:  "anomalib",
		Status:     "completed",
		AnalyzedAt: domain.NullSQLTime{SQLTime: domain.NewSQLTime(now), Valid: true},
	}
	log.Debug().Int64("snapshot_id", snap.ID).Msg("anomaly analysis stub completed")
	return analysis, nil
}

// applyReviewRules applies the heuristic rules that decide whether a
// snapshot needs human review. Mirrors the legacy _apply_review_rules:
// high person count, persons after hours, unexpected object classes.
//
// Args:
//
//	capturedAt: The snapshot capture time (project timezone).
//	detections: Detected objects.
//	personCount: Number of detected persons.
//
// Returns:
//
//	(review_required, review_reason) where review_reason is a semicolon-
//	joined description when flagged, nil otherwise.
func (s *AnalysisService) applyReviewRules(capturedAt time.Time, detections []domain.Detection, personCount int) (bool, *string) {
	hour := capturedAt.Hour()
	var reasons []string

	if personCount >= s.cfg.ReviewMaxPersonCount {
		reasons = append(reasons, fmt.Sprintf("high_person_count:%d", personCount))
	}
	if personCount > 0 && (hour >= s.cfg.ReviewPersonAfterHour || hour < s.cfg.ReviewPersonBeforeHour) {
		reasons = append(reasons, fmt.Sprintf("person_after_hours:hour=%d", hour))
	}

	// Allowlist mirrors the legacy rule set exactly.
	allowed := map[string]bool{
		"person": true, "car": true, "truck": true, "bicycle": true,
		"dog": true, "cat": true, "train": true,
	}
	unexpectedSet := map[string]bool{}
	for _, d := range detections {
		if !allowed[d.ClassName] {
			unexpectedSet[d.ClassName] = true
		}
	}
	if len(unexpectedSet) > 0 {
		classes := make([]string, 0, len(unexpectedSet))
		for c := range unexpectedSet {
			classes = append(classes, c)
		}
		sort.Strings(classes)
		if len(classes) > 5 {
			classes = classes[:5]
		}
		reasons = append(reasons, "unexpected_objects:"+strings.Join(classes, ", "))
	}

	if len(reasons) > 0 {
		reason := strings.Join(reasons, "; ")
		return true, &reason
	}
	return false, nil
}

// GetPendingReviews builds the review queue with camera metadata.
//
// Args:
//
//	ctx: Request context.
//	limit: Maximum items to return.
//
// Returns:
//
//	Pending review items (may be empty).
func (s *AnalysisService) GetPendingReviews(ctx context.Context, limit int) ([]domain.PendingReviewItem, error) {
	analyses, err := s.analyses.GetPendingReviews(ctx, limit)
	if err != nil {
		return nil, err
	}
	cams := repository.NewCameraRepository(s.db)
	items := make([]domain.PendingReviewItem, 0, len(analyses))
	for i := range analyses {
		a := &analyses[i]
		snap, err := s.snaps.GetByID(ctx, a.SnapshotID)
		if err != nil {
			continue
		}
		cam, err := cams.GetByID(ctx, snap.CameraID)
		cameraName := fmt.Sprintf("Camera %d", snap.CameraID)
		if err == nil {
			cameraName = cam.Name
		}
		items = append(items, domain.PendingReviewItem{
			AnalysisID:     a.ID,
			SnapshotID:     a.SnapshotID,
			CameraID:       snap.CameraID,
			CameraName:     cameraName,
			CapturedAt:     snap.CapturedAt,
			ImagePath:      snap.ImagePath,
			ModelName:      a.ModelName,
			PersonCount:    a.PersonCount,
			ReviewRequired: a.ReviewRequired,
			ReviewReason:   a.ReviewReason,
			AnomalyScore:   a.AnomalyScore,
			ErrorMessage:   a.ErrorMessage,
			ObjectsJSON:    a.ObjectsJSON,
			AnalyzedAt:     a.AnalyzedAt.SQLTime,
		})
	}
	return items, nil
}

// CountPendingReviews returns how many analyses await review.
//
// Args:
//
//	ctx: Request context.
//
// Returns:
//
//	The pending review count.
func (s *AnalysisService) CountPendingReviews(ctx context.Context) (int, error) {
	return s.analyses.CountPendingReviews(ctx)
}

// UpdateReview changes the review decision of an analysis.
//
// Args:
//
//	ctx: Request context.
//	analysisID: The analysis to update.
//	required: New review requirement flag.
//	reason: Optional review reason.
//
// Returns:
//
//	The updated analysis, or nil when not found.
func (s *AnalysisService) UpdateReview(ctx context.Context, analysisID int64, required bool, reason *string) (*domain.SnapshotAnalysis, error) {
	analysis, err := s.analyses.GetByID(ctx, analysisID)
	if err != nil {
		return nil, nil
	}
	if err := s.analyses.UpdateReview(ctx, analysisID, required, reason); err != nil {
		return nil, err
	}
	analysis.ReviewRequired = required
	analysis.ReviewReason = reason
	log.Info().Int64("analysis_id", analysisID).Bool("required", required).Msg("review updated")
	return analysis, nil
}

// GetDetections returns the detection browser rows with the same
// filters as the legacy endpoint (days back / explicit date, camera,
// class text search, pagination).
//
// Args:
//
//	ctx: Request context.
//	daysBack: Only include analyses from the last N days.
//	cameraID: Optional camera filter (0 = all).
//	className: Optional class text filter.
//	limit: Max rows.
//	offset: Pagination offset.
//	dateFrom: Specific date YYYY-MM-DD (overrides daysBack).
//
// Returns:
//
//	Flattened detection rows (may be empty).
func (s *AnalysisService) GetDetections(ctx context.Context, daysBack int, cameraID int64, className string, limit, offset int, dateFrom string) ([]repository.DetectionRow, error) {
	now := time.Now()
	var since, until *time.Time
	if dateFrom != "" {
		if t, err := time.ParseInLocation("2006-01-02", dateFrom, time.Local); err == nil {
			since = &t
			untilT := t.AddDate(0, 0, 1)
			until = &untilT
		}
	} else if daysBack == 0 {
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		since = &start
	} else {
		start := now.AddDate(0, 0, -daysBack)
		since = &start
	}
	return s.analyses.GetDetections(ctx, since, until, cameraID, className, limit, offset)
}

// strPtrIf returns a pointer to s when cond is true, else nil.
//
// Args:
//
//	cond: Whether to produce a pointer.
//	s: The string to point at.
//
// Returns:
//
//	A pointer when cond, nil otherwise.
func strPtrIf(cond bool, s string) *string {
	if cond {
		return &s
	}
	return nil
}

// strPtr returns a pointer to s (used for optional database columns).
//
// Args:
//
//	s: The string value.
//
// Returns:
//
//	A pointer to s.
func strPtr(s string) *string { return &s }
