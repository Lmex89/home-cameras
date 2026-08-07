package repository

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/Lmex89/home-cameras/internal/domain"
)

// SnapshotAnalysisRepository persists ML analysis results.
type SnapshotAnalysisRepository struct {
	db DBTX
}

// NewSnapshotAnalysisRepository builds a snapshot analysis repository.
func NewSnapshotAnalysisRepository(db DBTX) *SnapshotAnalysisRepository {
	return &SnapshotAnalysisRepository{db: db}
}

const analysisCols = `id, snapshot_id, model_name, model_version, status, objects_json,
	person_count, review_required, review_reason, anomaly_score, error_message,
	analyzed_at, created_at, updated_at`

// Add inserts a new analysis row and populates its id.
func (r *SnapshotAnalysisRepository) Add(ctx context.Context, a *domain.SnapshotAnalysis) error {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO snapshot_analyses (snapshot_id, model_name, model_version, status,
			objects_json, person_count, review_required, review_reason, anomaly_score,
			error_message, analyzed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.SnapshotID, a.ModelName, a.ModelVersion, a.Status, a.ObjectsJSON,
		a.PersonCount, a.ReviewRequired, a.ReviewReason, a.AnomalyScore,
		a.ErrorMessage, a.AnalyzedAt)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	a.ID = id
	return nil
}

// GetByID returns one analysis.
func (r *SnapshotAnalysisRepository) GetByID(ctx context.Context, id int64) (*domain.SnapshotAnalysis, error) {
	var a domain.SnapshotAnalysis
	err := sqlx.GetContext(ctx, r.db, &a,
		"SELECT "+analysisCols+" FROM snapshot_analyses WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetBySnapshot returns analyses for a snapshot, optionally by model.
func (r *SnapshotAnalysisRepository) GetBySnapshot(ctx context.Context, snapshotID int64, modelName ...string) ([]domain.SnapshotAnalysis, error) {
	query := "SELECT " + analysisCols + " FROM snapshot_analyses WHERE snapshot_id = ?"
	args := []any{snapshotID}
	if len(modelName) > 0 && modelName[0] != "" {
		query += " AND model_name = ?"
		args = append(args, modelName[0])
	}
	var out []domain.SnapshotAnalysis
	err := sqlx.SelectContext(ctx, r.db, &out, query, args...)
	return out, err
}

// GetPendingReviews returns analyses flagged for human review.
func (r *SnapshotAnalysisRepository) GetPendingReviews(ctx context.Context, limit int) ([]domain.SnapshotAnalysis, error) {
	var out []domain.SnapshotAnalysis
	err := sqlx.SelectContext(ctx, r.db, &out,
		"SELECT "+analysisCols+` FROM snapshot_analyses
		 WHERE review_required = 1
		 ORDER BY analyzed_at DESC LIMIT ?`, limit)
	return out, err
}

// GetByCameraAndDate returns analyses for a set of snapshot IDs.
func (r *SnapshotAnalysisRepository) GetByCameraAndDate(ctx context.Context, snapshotIDs []int64) ([]domain.SnapshotAnalysis, error) {
	if len(snapshotIDs) == 0 {
		return nil, nil
	}
	query, args, err := sqlx.In(
		"SELECT "+analysisCols+` FROM snapshot_analyses
		 WHERE snapshot_id IN (?) ORDER BY analyzed_at DESC`, snapshotIDs)
	if err != nil {
		return nil, err
	}
	var out []domain.SnapshotAnalysis
	err = sqlx.SelectContext(ctx, r.db, &out, r.db.Rebind(query), args...)
	return out, err
}

// CountPendingReviews counts flagged analyses.
func (r *SnapshotAnalysisRepository) CountPendingReviews(ctx context.Context) (int, error) {
	var n int
	err := sqlx.GetContext(ctx, r.db, &n,
		"SELECT COUNT(id) FROM snapshot_analyses WHERE review_required = 1")
	return n, err
}

// UpdateReview sets the review flag and optional reason.
func (r *SnapshotAnalysisRepository) UpdateReview(ctx context.Context, id int64, required bool, reason *string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE snapshot_analyses SET review_required = ?, review_reason = ?,
		 updated_at = CURRENT_TIMESTAMP WHERE id = ?`, required, reason, id)
	return err
}

// GetDetections returns analyses with detections joined to snapshots
// and cameras, mirroring the legacy detections browser query.
func (r *SnapshotAnalysisRepository) GetDetections(ctx context.Context, since, until *time.Time, cameraID int64, className string, limit, offset int) ([]DetectionRow, error) {
	query := `SELECT sa.id AS analysis_id, sa.snapshot_id, s.camera_id,
		c.name AS camera_name, s.captured_at, s.image_path,
		sa.model_name, sa.objects_json, sa.person_count,
		sa.review_required, sa.review_reason, sa.analyzed_at
		FROM snapshot_analyses sa
		JOIN snapshots s ON s.id = sa.snapshot_id
		JOIN cameras c ON c.id = s.camera_id
		WHERE sa.objects_json IS NOT NULL`
	args := make([]any, 0, 6)
	if since != nil {
		query += " AND sa.analyzed_at >= ?"
		args = append(args, since.Format("2006-01-02 15:04:05"))
	}
	if until != nil {
		query += " AND sa.analyzed_at < ?"
		args = append(args, until.Format("2006-01-02 15:04:05"))
	}
	if cameraID > 0 {
		query += " AND s.camera_id = ?"
		args = append(args, cameraID)
	}
	if className != "" {
		query += " AND sa.objects_json LIKE ?"
		args = append(args, "%"+className+"%")
	}
	query += " ORDER BY sa.analyzed_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	var rows []DetectionRow
	if err := sqlx.SelectContext(ctx, r.db, &rows, query, args...); err != nil {
		return nil, err
	}
	return rows, nil
}

// DeleteBySnapshotIDs deletes analyses for the given snapshots.
func (r *SnapshotAnalysisRepository) DeleteBySnapshotIDs(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	query, args, err := sqlx.In("DELETE FROM snapshot_analyses WHERE snapshot_id IN (?)", ids)
	if err != nil {
		return 0, err
	}
	res, err := r.db.ExecContext(ctx, r.db.Rebind(query), args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DetectionRow is a flattened detection result for the API.
type DetectionRow struct {
	AnalysisID     int64          `db:"analysis_id" json:"analysis_id"`
	SnapshotID     int64          `db:"snapshot_id" json:"snapshot_id"`
	CameraID       int64          `db:"camera_id" json:"camera_id"`
	CameraName     string         `db:"camera_name" json:"camera_name"`
	CapturedAt     domain.SQLTime `db:"captured_at" json:"captured_at"`
	ImagePath      string         `db:"image_path" json:"image_path"`
	ModelName      string         `db:"model_name" json:"model_name"`
	ObjectsJSON    *string        `db:"objects_json" json:"objects_json"`
	PersonCount    int            `db:"person_count" json:"person_count"`
	ReviewRequired bool           `db:"review_required" json:"review_required"`
	ReviewReason   *string        `db:"review_reason" json:"review_reason"`
	AnalyzedAt     domain.SQLTime `db:"analyzed_at" json:"analyzed_at"`
}
