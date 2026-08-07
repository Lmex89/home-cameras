// Package domain defines the persistence entities of the camera
// monitor as plain structs with sqlx tags, plus the SQLite-compatible
// timestamp type shared across the application.
package domain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// sqliteTimeLayout matches the text format produced by SQLite's
// CURRENT_TIMESTAMP (and the naive local datetimes the legacy Python
// app stored). The zero-padded YYYY-MM-DD HH:MM:SS ordering makes
// string comparisons in SQL lexicographically chronological.
const sqliteTimeLayout = "2006-01-02 15:04:05"

// extra layouts accepted when scanning (fractional seconds, ISO T).
var scanLayouts = []string{
	sqliteTimeLayout,
	"2006-01-02 15:04:05.000",
	"2006-01-02 15:04:05.000000",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05.000Z07:00",
}

// SQLTime wraps time.Time so it can be scanned from and bound to the
// SQLite text format used by the schema. Stored values carry no zone
// info; they are interpreted in time.Local (the project timezone,
// configured at startup) to mirror the legacy semantics.
type SQLTime struct {
	time.Time
}

// NewSQLTime wraps a time.Time.
func NewSQLTime(t time.Time) SQLTime { return SQLTime{Time: t} }

// Now returns the current time in the project location.
func Now() SQLTime { return SQLTime{Time: time.Now()} }

// Scan implements sql.Scanner.
func (t *SQLTime) Scan(v any) error {
	if v == nil {
		t.Time = time.Time{}
		return nil
	}
	switch s := v.(type) {
	case time.Time:
		t.Time = s
		return nil
	case string:
		for _, layout := range scanLayouts {
			if parsed, err := time.ParseInLocation(layout, s, time.Local); err == nil {
				t.Time = parsed
				return nil
			}
		}
		return fmt.Errorf("unsupported timestamp string %q", s)
	case []byte:
		return t.Scan(string(s))
	default:
		return fmt.Errorf("unsupported timestamp type %T", v)
	}
}

// Value implements driver.Valuer.
func (t SQLTime) Value() (driver.Value, error) {
	if t.IsZero() {
		return nil, nil
	}
	return t.Format(sqliteTimeLayout), nil
}

// MarshalJSON emits an ISO-8601 string (parity with Python's isoformat).
func (t SQLTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Time.Format("2006-01-02T15:04:05"))
}

// NullSQLTime is a nullable variant used by optional columns.
type NullSQLTime struct {
	SQLTime
	Valid bool
}

// Scan implements sql.Scanner.
func (t *NullSQLTime) Scan(v any) error {
	if v == nil {
		t.Valid = false
		t.Time = time.Time{}
		return nil
	}
	if err := t.SQLTime.Scan(v); err != nil {
		return err
	}
	t.Valid = true
	return nil
}

// Value implements driver.Valuer.
func (t NullSQLTime) Value() (driver.Value, error) {
	if !t.Valid {
		return nil, nil
	}
	return t.SQLTime.Value()
}

// Camera is an ONVIF-capable camera under monitoring.
type Camera struct {
	ID              int64   `db:"id" json:"id"`
	Name            string  `db:"name" json:"name"`
	Host            string  `db:"host" json:"host"`
	Port            int     `db:"port" json:"port"`
	Username        string  `db:"username" json:"username"`
	Password        string  `db:"password" json:"password"`
	ProfileToken    *string `db:"profile_token" json:"profile_token"`
	SnapshotURL     *string `db:"snapshot_url" json:"snapshot_url"`
	IntervalSeconds int     `db:"interval_seconds" json:"interval_seconds"`
	Enabled         bool    `db:"enabled" json:"enabled"`
	CreatedAt       SQLTime `db:"created_at" json:"created_at"`
	UpdatedAt       SQLTime `db:"updated_at" json:"updated_at"`
}

// Snapshot is a single captured image (or failed capture attempt).
type Snapshot struct {
	ID           int64   `db:"id" json:"id"`
	CameraID     int64   `db:"camera_id" json:"camera_id"`
	CapturedAt   SQLTime `db:"captured_at" json:"captured_at"`
	ImagePath    string  `db:"image_path" json:"image_path"`
	FileSize     int64   `db:"file_size" json:"file_size"`
	Status       string  `db:"status" json:"status"`
	ErrorMessage *string `db:"error_message" json:"error_message"`
	ArchivePath  *string `db:"archive_path" json:"archive_path"`
}

// AnalysisJob is a queued ML inference work item for a snapshot.
type AnalysisJob struct {
	ID           int64       `db:"id" json:"id"`
	SnapshotID   int64       `db:"snapshot_id" json:"snapshot_id"`
	JobType      string      `db:"job_type" json:"job_type"`
	Status       string      `db:"status" json:"status"`
	Priority     int         `db:"priority" json:"priority"`
	Attempts     int         `db:"attempts" json:"attempts"`
	MaxAttempts  int         `db:"max_attempts" json:"max_attempts"`
	ErrorMessage *string     `db:"error_message" json:"error_message"`
	RequestedAt  SQLTime     `db:"requested_at" json:"requested_at"`
	StartedAt    NullSQLTime `db:"started_at" json:"started_at"`
	FinishedAt   NullSQLTime `db:"finished_at" json:"finished_at"`
}

// SnapshotAnalysis stores the ML result for one snapshot + model pair.
type SnapshotAnalysis struct {
	ID             int64       `db:"id" json:"id"`
	SnapshotID     int64       `db:"snapshot_id" json:"snapshot_id"`
	ModelName      string      `db:"model_name" json:"model_name"`
	ModelVersion   string      `db:"model_version" json:"model_version"`
	Status         string      `db:"status" json:"status"`
	ObjectsJSON    *string     `db:"objects_json" json:"objects_json"`
	PersonCount    int         `db:"person_count" json:"person_count"`
	ReviewRequired bool        `db:"review_required" json:"review_required"`
	ReviewReason   *string     `db:"review_reason" json:"review_reason"`
	AnomalyScore   *float64    `db:"anomaly_score" json:"anomaly_score"`
	ErrorMessage   *string     `db:"error_message" json:"error_message"`
	AnalyzedAt     NullSQLTime `db:"analyzed_at" json:"analyzed_at"`
	CreatedAt      SQLTime     `db:"created_at" json:"created_at"`
	UpdatedAt      SQLTime     `db:"updated_at" json:"updated_at"`
}
