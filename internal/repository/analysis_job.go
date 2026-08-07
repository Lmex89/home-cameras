package repository

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/Lmex89/home-cameras/internal/domain"
)

// AnalysisJobRepository persists analysis jobs and queue state.
type AnalysisJobRepository struct {
	db DBTX
}

// NewAnalysisJobRepository builds an analysis job repository.
func NewAnalysisJobRepository(db DBTX) *AnalysisJobRepository { return &AnalysisJobRepository{db: db} }

const jobCols = `id, snapshot_id, job_type, status, priority, attempts, max_attempts,
	error_message, requested_at, started_at, finished_at`

// Add inserts a new pending job and populates its id.
func (r *AnalysisJobRepository) Add(ctx context.Context, j *domain.AnalysisJob) error {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO analysis_jobs (snapshot_id, job_type, status, priority)
		 VALUES (?, ?, ?, ?)`,
		j.SnapshotID, j.JobType, j.Status, j.Priority)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	j.ID = id
	return nil
}

// GetByID returns one job.
func (r *AnalysisJobRepository) GetByID(ctx context.Context, id int64) (*domain.AnalysisJob, error) {
	var j domain.AnalysisJob
	err := sqlx.GetContext(ctx, r.db, &j,
		"SELECT "+jobCols+" FROM analysis_jobs WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// GetPending returns pending jobs by priority then age.
func (r *AnalysisJobRepository) GetPending(ctx context.Context, limit int) ([]domain.AnalysisJob, error) {
	var jobs []domain.AnalysisJob
	err := sqlx.SelectContext(ctx, r.db, &jobs,
		"SELECT "+jobCols+` FROM analysis_jobs
		 WHERE status = 'pending'
		 ORDER BY priority DESC, requested_at LIMIT ?`, limit)
	return jobs, err
}

// MarkStarted transitions a job to processing and bumps attempts.
func (r *AnalysisJobRepository) MarkStarted(ctx context.Context, id int64) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := r.db.ExecContext(ctx,
		`UPDATE analysis_jobs SET status = 'processing', started_at = ?,
		 attempts = attempts + 1 WHERE id = ?`, now, id)
	return err
}

// MarkCompleted marks a job done.
func (r *AnalysisJobRepository) MarkCompleted(ctx context.Context, id int64) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := r.db.ExecContext(ctx,
		"UPDATE analysis_jobs SET status = 'completed', finished_at = ? WHERE id = ?", now, id)
	return err
}

// MarkFailed marks a job failed with a message.
func (r *AnalysisJobRepository) MarkFailed(ctx context.Context, id int64, msg string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := r.db.ExecContext(ctx,
		"UPDATE analysis_jobs SET status = 'failed', error_message = ?, finished_at = ? WHERE id = ?",
		msg, now, id)
	return err
}

// DeleteBySnapshotIDs removes jobs for the given snapshots.
func (r *AnalysisJobRepository) DeleteBySnapshotIDs(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	query, args, err := sqlx.In("DELETE FROM analysis_jobs WHERE snapshot_id IN (?)", ids)
	if err != nil {
		return 0, err
	}
	res, err := r.db.ExecContext(ctx, r.db.Rebind(query), args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
