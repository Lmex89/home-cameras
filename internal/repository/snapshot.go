package repository

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/Lmex89/home-cameras/internal/domain"
)

// SnapshotRepository persists Snapshot entities.
type SnapshotRepository struct {
	db DBTX
}

// NewSnapshotRepository builds a snapshot repository bound to db.
func NewSnapshotRepository(db DBTX) *SnapshotRepository { return &SnapshotRepository{db: db} }

const snapshotCols = `id, camera_id, captured_at, image_path, file_size, status,
	error_message, archive_path`

// GetByID returns a snapshot or an error when missing.
func (r *SnapshotRepository) GetByID(ctx context.Context, id int64) (*domain.Snapshot, error) {
	var s domain.Snapshot
	err := sqlx.GetContext(ctx, r.db, &s,
		"SELECT "+snapshotCols+" FROM snapshots WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetByCameraAndDate returns a camera's snapshots for one full day.
func (r *SnapshotRepository) GetByCameraAndDate(ctx context.Context, cameraID int64, day time.Time) ([]domain.Snapshot, error) {
	start := dayInLocation(day, true)
	end := dayInLocation(day, false)
	var snaps []domain.Snapshot
	err := sqlx.SelectContext(ctx, r.db, &snaps,
		"SELECT "+snapshotCols+` FROM snapshots
		 WHERE camera_id = ? AND captured_at >= ? AND captured_at <= ?
		 ORDER BY captured_at`, cameraID, start, end)
	return snaps, err
}

// GetByDate returns all snapshots across cameras for one full day.
func (r *SnapshotRepository) GetByDate(ctx context.Context, day time.Time) ([]domain.Snapshot, error) {
	start := dayInLocation(day, true)
	end := dayInLocation(day, false)
	var snaps []domain.Snapshot
	err := sqlx.SelectContext(ctx, r.db, &snaps,
		"SELECT "+snapshotCols+` FROM snapshots
		 WHERE captured_at >= ? AND captured_at <= ?
		 ORDER BY camera_id, captured_at`, start, end)
	return snaps, err
}

// GetLastByCamera returns the most recent successful snapshot.
func (r *SnapshotRepository) GetLastByCamera(ctx context.Context, cameraID int64) (*domain.Snapshot, error) {
	var s domain.Snapshot
	err := sqlx.GetContext(ctx, r.db, &s,
		"SELECT "+snapshotCols+` FROM snapshots
		 WHERE camera_id = ? AND status = 'success'
		 ORDER BY captured_at DESC LIMIT 1`, cameraID)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetLastForAllCameras returns the latest successful snapshot per camera.
func (r *SnapshotRepository) GetLastForAllCameras(ctx context.Context, cameraIDs []int64) (map[int64]*domain.Snapshot, error) {
	out := make(map[int64]*domain.Snapshot, len(cameraIDs))
	if len(cameraIDs) == 0 {
		return out, nil
	}
	query, args, err := sqlx.In(
		"SELECT "+snapshotCols+` FROM snapshots
		 WHERE camera_id IN (?) AND status = 'success'
		 ORDER BY captured_at DESC`, cameraIDs)
	if err != nil {
		return nil, err
	}
	var snaps []domain.Snapshot
	if err := sqlx.SelectContext(ctx, r.db, &snaps, r.db.Rebind(query), args...); err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	for _, s := range snaps {
		if !seen[s.CameraID] {
			seen[s.CameraID] = true
			cp := s
			out[s.CameraID] = &cp
		}
	}
	return out, nil
}

// Add inserts a snapshot and populates its generated id.
func (r *SnapshotRepository) Add(ctx context.Context, s *domain.Snapshot) error {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO snapshots (camera_id, captured_at, image_path, file_size, status, error_message)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		s.CameraID, s.CapturedAt, s.ImagePath, s.FileSize, s.Status, s.ErrorMessage)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	s.ID = id
	return nil
}

// GetAllSuccessfulWithImages returns successful snapshots with files.
func (r *SnapshotRepository) GetAllSuccessfulWithImages(ctx context.Context) ([]domain.Snapshot, error) {
	var snaps []domain.Snapshot
	err := sqlx.SelectContext(ctx, r.db, &snaps,
		"SELECT "+snapshotCols+` FROM snapshots
		 WHERE status = 'success' AND image_path != ''
		 ORDER BY camera_id, captured_at`)
	return snaps, err
}

// DeleteOlderThan deletes snapshots captured before cutoff.
func (r *SnapshotRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM snapshots WHERE captured_at < ?", cutoff.Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return n, err
}

// GetOldUnarchived returns snapshots older than cutoff without archives.
func (r *SnapshotRepository) GetOldUnarchived(ctx context.Context, cutoff time.Time) ([]domain.Snapshot, error) {
	var snaps []domain.Snapshot
	err := sqlx.SelectContext(ctx, r.db, &snaps,
		"SELECT "+snapshotCols+` FROM snapshots
		 WHERE captured_at < ? AND archive_path IS NULL
		 ORDER BY camera_id, captured_at`, cutoff.Format("2006-01-02 15:04:05"))
	return snaps, err
}

// UpdateArchivePath sets the archive reference for a snapshot.
func (r *SnapshotRepository) UpdateArchivePath(ctx context.Context, id int64, ref string) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE snapshots SET archive_path = ? WHERE id = ?", ref, id)
	return err
}

// MarkArchivedBatch sets the same archive ref on many snapshots.
func (r *SnapshotRepository) MarkArchivedBatch(ctx context.Context, ids []int64, ref string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	query, args, err := sqlx.In(
		"UPDATE snapshots SET archive_path = ? WHERE id IN (?)", ref, ids)
	if err != nil {
		return 0, err
	}
	res, err := r.db.ExecContext(ctx, r.db.Rebind(query), args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountByArchiveZip counts snapshots referencing an archive prefix.
func (r *SnapshotRepository) CountByArchiveZip(ctx context.Context, zipPath string) (int, error) {
	var n int
	err := sqlx.GetContext(ctx, r.db, &n,
		"SELECT COUNT(id) FROM snapshots WHERE archive_path LIKE ?", zipPath+"%")
	return n, err
}

// dayInLocation returns the start (or exclusive end) of a day.
func dayInLocation(day time.Time, start bool) string {
	y, m, d := day.Date()
	t := time.Date(y, m, d, 0, 0, 0, 0, day.Location())
	if !start {
		t = t.AddDate(0, 0, 1)
	}
	return t.Format("2006-01-02 15:04:05")
}
