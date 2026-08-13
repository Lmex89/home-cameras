package domain

// API request/response schemas mirroring the Pydantic v2 schemas of the
// legacy application. Validation happens in the handler layer (bounded
// fields, required strings) before reaching the services.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CameraCreate is the payload for creating a camera.
type CameraCreate struct {
	Name            string  `json:"name"`
	CameraType      string  `json:"camera_type"`
	Host            string  `json:"host"`
	Port            int     `json:"port"`
	Username        string  `json:"username"`
	Password        string  `json:"password"`
	ProfileToken    *string `json:"profile_token"`
	SnapshotURL     *string `json:"snapshot_url"`
	DevicePath      *string `json:"device_path"`
	IntervalSeconds int     `json:"interval_seconds"`
	Enabled         bool    `json:"enabled"`
}

// CameraUpdate is the payload for partially updating a camera. Pointer
// fields distinguish "unset" from "explicitly cleared".
type CameraUpdate struct {
	Name            *string `json:"name"`
	CameraType      *string `json:"camera_type"`
	Host            *string `json:"host"`
	Port            *int    `json:"port"`
	Username        *string `json:"username"`
	Password        *string `json:"password"`
	ProfileToken    *string `json:"profile_token"`
	SnapshotURL     *string `json:"snapshot_url"`
	DevicePath      *string `json:"device_path"`
	IntervalSeconds *int    `json:"interval_seconds"`
	Enabled         *bool   `json:"enabled"`
}

// CameraTestResult reports ONVIF reachability for an untested payload.
type CameraTestResult struct {
	Reachable bool     `json:"reachable"`
	Profiles  []string `json:"profiles"`
	Error     *string  `json:"error"`
}

// SnapshotWithAnalysis enriches a snapshot with its ML analysis.
type SnapshotWithAnalysis struct {
	Snapshot
	Analysis *SnapshotAnalysis `json:"analysis"`
}

// CameraWithLastSnapshot augments a camera with dashboard metrics.
type CameraWithLastSnapshot struct {
	Camera
	LastSnapshot   *Snapshot `json:"last_snapshot"`
	TotalSnapshots int       `json:"total_snapshots"`
}

// SnapshotForceResult reports an on-demand capture outcome.
type SnapshotForceResult struct {
	CameraID   int64   `json:"camera_id"`
	CameraName string  `json:"camera_name"`
	Success    bool    `json:"success"`
	ImagePath  *string `json:"image_path"`
	Error      *string `json:"error"`
}

// DailyReportCamera groups one camera's snapshots for a date.
type DailyReportCamera struct {
	CameraID       int64                  `json:"camera_id"`
	CameraName     string                 `json:"camera_name"`
	TotalSnapshots int                    `json:"total_snapshots"`
	Snapshots      []SnapshotWithAnalysis `json:"snapshots"`
}

// DailyReport is the full daily report response.
type DailyReport struct {
	Date    string              `json:"date"`
	Cameras []DailyReportCamera `json:"cameras"`
}

// Day is a calendar date serialized as YYYY-MM-DD (the legacy Pydantic
// date type). It accepts both "YYYY-MM-DD" and RFC3339 timestamps on
// input and always emits "YYYY-MM-DD" on output.
type Day time.Time

// UnmarshalJSON accepts "2006-01-02" (and RFC3339) values.
func (d *Day) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(string(data), `"`)
	if raw == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			*d = Day(t)
			return nil
		}
	}
	return fmt.Errorf("invalid date %q (want YYYY-MM-DD)", raw)
}

// MarshalJSON emits "YYYY-MM-DD".
func (d Day) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(d).Format("2006-01-02"))
}

// Time converts the day to a time.Time at midnight in the project zone.
func (d Day) Time() time.Time { return time.Time(d) }

// VideoRequest asks for a plain timelapse for a camera/date.
type VideoRequest struct {
	CameraID int64 `json:"camera_id"`
	Date     Day   `json:"date"`
	Hour     *int  `json:"hour"`
}

// VideoResponse returns the download URL of a generated video.
type VideoResponse struct {
	VideoURL string `json:"video_url"`
}

// AnnotatedVideoRequest asks for an annotated timelapse.
type AnnotatedVideoRequest struct {
	CameraID int64   `json:"camera_id"`
	Date     Day     `json:"date"`
	Classes  *string `json:"classes"`
}

// AnnotatedVideoResponse mirrors VideoResponse for the annotated flow.
type AnnotatedVideoResponse struct {
	VideoURL string `json:"video_url"`
}

// AnalysisReviewUpdate changes the review flag of an analysis.
type AnalysisReviewUpdate struct {
	ReviewRequired bool    `json:"review_required"`
	ReviewReason   *string `json:"review_reason"`
}

// BulkReviewPayload updates several analyses at once.
type BulkReviewPayload struct {
	AnalysisIDs    []int64 `json:"analysis_ids"`
	ReviewRequired bool    `json:"review_required"`
	ReviewReason   *string `json:"review_reason"`
}

// PendingReviewItem lists one flagged snapshot for human review.
type PendingReviewItem struct {
	AnalysisID     int64    `json:"analysis_id"`
	SnapshotID     int64    `json:"snapshot_id"`
	CameraID       int64    `json:"camera_id"`
	CameraName     string   `json:"camera_name"`
	CapturedAt     SQLTime  `json:"captured_at"`
	ImagePath      string   `json:"image_path"`
	ModelName      string   `json:"model_name"`
	PersonCount    int      `json:"person_count"`
	ReviewRequired bool     `json:"review_required"`
	ReviewReason   *string  `json:"review_reason"`
	AnomalyScore   *float64 `json:"anomaly_score"`
	ErrorMessage   *string  `json:"error_message"`
	ObjectsJSON    *string  `json:"objects_json"`
	AnalyzedAt     SQLTime  `json:"analyzed_at"`
}

// PurgeRequest asks to delete everything older than N days.
type PurgeRequest struct {
	Days int `json:"days"`
}

// Detection is one YOLO detection result.
type Detection struct {
	ClassName  string    `json:"class_name"`
	Confidence float64   `json:"confidence"`
	BBox       []float64 `json:"bbox"`
}
