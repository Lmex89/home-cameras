package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/Lmex89/home-cameras/internal/domain"
)

// CameraRepository persists Camera entities.
type CameraRepository struct {
	db DBTX
}

// NewCameraRepository builds a camera repository bound to db.
func NewCameraRepository(db DBTX) *CameraRepository { return &CameraRepository{db: db} }

// GetAll returns all cameras ordered by name.
func (r *CameraRepository) GetAll(ctx context.Context) ([]domain.Camera, error) {
	var cams []domain.Camera
	err := sqlx.SelectContext(ctx, r.db, &cams,
		`SELECT id, name, host, port, username, password, profile_token,
		        snapshot_url, interval_seconds, enabled, created_at, updated_at
		 FROM cameras ORDER BY name`)
	return cams, err
}

// GetEnabled returns enabled cameras ordered by name.
func (r *CameraRepository) GetEnabled(ctx context.Context) ([]domain.Camera, error) {
	var cams []domain.Camera
	err := sqlx.SelectContext(ctx, r.db, &cams,
		`SELECT id, name, host, port, username, password, profile_token,
		        snapshot_url, interval_seconds, enabled, created_at, updated_at
		 FROM cameras WHERE enabled = 1 ORDER BY name`)
	return cams, err
}

// GetByID returns a single camera or ErrNotFound.
func (r *CameraRepository) GetByID(ctx context.Context, id int64) (*domain.Camera, error) {
	var cam domain.Camera
	err := sqlx.GetContext(ctx, r.db, &cam,
		`SELECT id, name, host, port, username, password, profile_token,
		        snapshot_url, interval_seconds, enabled, created_at, updated_at
		 FROM cameras WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &cam, nil
}

// Add inserts a new camera and populates its generated id plus the
// server-side timestamps (SQLite CURRENT_TIMESTAMP defaults are not
// returned by LastInsertId, so the row is re-read).
func (r *CameraRepository) Add(ctx context.Context, cam *domain.Camera) error {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO cameras (name, host, port, username, password, profile_token,
		                     snapshot_url, interval_seconds, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cam.Name, cam.Host, cam.Port, cam.Username, cam.Password,
		cam.ProfileToken, cam.SnapshotURL, cam.IntervalSeconds, cam.Enabled)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	cam.ID = id
	return sqlx.GetContext(ctx, r.db, cam,
		`SELECT id, name, host, port, username, password, profile_token,
		        snapshot_url, interval_seconds, enabled, created_at, updated_at
		 FROM cameras WHERE id = ?`, id)
}

// Update applies a partial column update and returns the refreshed row.
func (r *CameraRepository) Update(ctx context.Context, id int64, values map[string]any) (*domain.Camera, error) {
	if len(values) == 0 {
		return r.GetByID(ctx, id)
	}
	cols := make([]string, 0, len(values))
	args := make([]any, 0, len(values)+1)
	for col, val := range values {
		cols = append(cols, col+" = ?")
		args = append(args, val)
	}
	args = append(args, id)
	stmt := fmt.Sprintf("UPDATE cameras SET %s, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		strings.Join(cols, ", "))
	if _, err := r.db.ExecContext(ctx, stmt, args...); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

// Delete removes a camera by id, returning whether a row existed.
func (r *CameraRepository) Delete(ctx context.Context, id int64) (bool, error) {
	res, err := r.db.ExecContext(ctx, "DELETE FROM cameras WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
