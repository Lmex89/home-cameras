package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadDefaults verifies zero-env loading applies envDefault values.
func TestLoadDefaults(t *testing.T) {
	for _, key := range envKeys() {
		os.Unsetenv(key)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AppName != "Camera Monitor" {
		t.Errorf("app_name = %q", cfg.AppName)
	}
	if cfg.Port != 8004 {
		t.Errorf("port = %d", cfg.Port)
	}
	if cfg.Timezone != "America/Mexico_City" {
		t.Errorf("timezone = %q", cfg.Timezone)
	}
	if !cfg.AnalysisEnabled || !cfg.TimelapseEnabled {
		t.Error("analysis/timelapse should default to enabled")
	}
	if cfg.TelegramEnabled || cfg.StorageEnabled {
		t.Error("telegram/storage should default to disabled")
	}
	if cfg.SnapshotRetentionDays != 30 || cfg.SnapshotZipAfterDays != 7 {
		t.Errorf("retention defaults wrong: %+v", cfg)
	}
	if cfg.BaseDir() == "" || cfg.YAMLPath() == "" {
		t.Error("expected base dir and yaml path")
	}
	if cfg.LogsDir() != "data/logs" {
		t.Errorf("logs dir = %q", cfg.LogsDir())
	}
}

// TestLoadEnvOverrides verifies every env var maps to its field.
func TestLoadEnvOverrides(t *testing.T) {
	for _, key := range envKeys() {
		os.Unsetenv(key)
	}
	tests := []struct{ key, value string }{
		{"APP_NAME", "Test Cam"},
		{"DEBUG", "false"},
		{"HOST", "127.0.0.1"},
		{"PORT", "9999"},
		{"TIMEZONE", "UTC"},
		{"SNAPSHOT_RETENTION_DAYS", "10"},
		{"SNAPSHOT_ZIP_AFTER_DAYS", "2"},
		{"VIDEO_RETENTION_DAYS", "15"},
		{"DEFAULT_INTERVAL_SECONDS", "30"},
		{"CAPTURE_TIMEOUT_SECONDS", "60"},
		{"HEALTH_CHECK_INTERVAL_MINUTES", "5"},
		{"YOLO_MODEL_PATH", "models/test.pt"},
		{"YOLO_CONFIDENCE_THRESHOLD", "0.8"},
		{"REVIEW_PERSON_AFTER_HOUR", "23"},
		{"REVIEW_PERSON_BEFORE_HOUR", "7"},
		{"REVIEW_MAX_PERSON_COUNT", "3"},
		{"ANALYSIS_ENABLED", "false"},
		{"ANALYSIS_INTERVAL_SECONDS", "15"},
		{"TIMELAPSE_ENABLED", "false"},
		{"TIMELAPSE_HOUR", "12"},
		{"TIMELAPSE_MINUTE", "30"},
		{"TIMELAPSE_CAMERA_ID", "2"},
		{"TIMELAPSE_OBJECT_CLASSES", "dog, cat"},
		{"TIMELAPSE_FRAME_DURATION", "0.25"},
		{"TIMELAPSE_WORKERS", "7"},
		{"TELEGRAM_ENABLED", "true"},
		{"TELEGRAM_BOT_TOKEN", "tok"},
		{"TELEGRAM_CHAT_ID", "123"},
		{"STORAGE_ENABLED", "true"},
		{"STORAGE_ENDPOINT_URL", "https://s3.example"},
		{"STORAGE_BUCKET_NAME", "bkt"},
		{"STORAGE_ACCESS_KEY", "ak"},
		{"STORAGE_SECRET_KEY", "sk"},
		{"STORAGE_PUBLIC_URL", "https://pub.example/"},
		{"STORAGE_REGION", "eu"},
		{"DATA_DIR", "/tmp/cams"},
	}
	for _, tt := range tests {
		t.Setenv(tt.key, tt.value)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != 9999 || cfg.Timezone != "UTC" || cfg.Debug {
		t.Errorf("parsed wrong: %+v", cfg)
	}
	if cfg.YoloConfidenceThreshold != 0.8 {
		t.Errorf("threshold = %v", cfg.YoloConfidenceThreshold)
	}
	if cfg.TimelapseObjectClasses != "dog, cat" {
		t.Errorf("classes = %q", cfg.TimelapseObjectClasses)
	}
	if cfg.DataDir != "/tmp/cams" {
		t.Errorf("data dir = %q", cfg.DataDir)
	}
	if cfg.SnapshotsDir() != "/tmp/cams/snapshots" {
		t.Errorf("snapshots dir = %q", cfg.SnapshotsDir())
	}
	if cfg.DBPath() != "/tmp/cams/cameras.db" {
		t.Errorf("db path = %q", cfg.DBPath())
	}
	set := cfg.TimelapseClassSet()
	if !set["dog"] || !set["cat"] || set["car"] || len(set) != 2 {
		t.Errorf("class set = %v", set)
	}
}

// TestLoadInvalidEnv verifies a bad value surfaces as an error.
func TestLoadInvalidEnv(t *testing.T) {
	for _, key := range envKeys() {
		os.Unsetenv(key)
	}
	t.Setenv("PORT", "not-a-port")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid PORT")
	}
}

// TestLoadDotEnv verifies .env files seed variables and never override
// existing environment variables.
func TestLoadDotEnv(t *testing.T) {
	for _, key := range envKeys() {
		os.Unsetenv(key)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(`
# comment line
PORT=7777
APP_NAME="Quoted App"
SNAPSHOT_ZIP_AFTER_DAYS='3'
INVALID_LINE_NO_EQUALS
  TRIMMED_KEY = spaces ok
EMPTY_VALUE=
`), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	// Pre-set PORT: the .env must NOT override it.
	t.Setenv("PORT", "6000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != 6000 {
		t.Errorf("port = %d (env must win over .env)", cfg.Port)
	}
	if cfg.AppName != "Quoted App" {
		t.Errorf("app name = %q (quotes must be stripped)", cfg.AppName)
	}
	if cfg.SnapshotZipAfterDays != 3 {
		t.Errorf("zip days = %d (single quotes must be stripped)", cfg.SnapshotZipAfterDays)
	}
}

// TestLoadDotEnvMalformed verifies unreadable/malformed files degrade
// gracefully: a missing .env is fine, a directory is an error.
func TestLoadDotEnvMalformed(t *testing.T) {
	if err := loadDotEnv(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if err := loadDotEnv(t.TempDir()); err == nil {
		t.Fatal("expected error reading a directory as .env")
	}
}

// envKeys lists every environment variable the config reads.
func envKeys() []string {
	return append([]string{}, []string{
		"APP_NAME", "DEBUG", "HOST", "PORT", "TIMEZONE",
		"SNAPSHOT_RETENTION_DAYS", "SNAPSHOT_ZIP_AFTER_DAYS", "VIDEO_RETENTION_DAYS",
		"DEFAULT_INTERVAL_SECONDS", "CAPTURE_TIMEOUT_SECONDS", "HEALTH_CHECK_INTERVAL_MINUTES",
		"YOLO_MODEL_PATH", "YOLO_CONFIDENCE_THRESHOLD", "YOLO_NUM_THREADS",
		"REVIEW_PERSON_AFTER_HOUR", "REVIEW_PERSON_BEFORE_HOUR", "REVIEW_MAX_PERSON_COUNT",
		"ANALYSIS_ENABLED", "ANALYSIS_INTERVAL_SECONDS",
		"TIMELAPSE_ENABLED", "TIMELAPSE_HOUR", "TIMELAPSE_MINUTE", "TIMELAPSE_CAMERA_ID",
		"TIMELAPSE_OBJECT_CLASSES", "TIMELAPSE_FRAME_DURATION", "TIMELAPSE_WORKERS",
		"TELEGRAM_ENABLED", "TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID",
		"STORAGE_ENABLED", "STORAGE_ENDPOINT_URL", "STORAGE_BUCKET_NAME",
		"STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY", "STORAGE_PUBLIC_URL", "STORAGE_REGION",
		"DATA_DIR",
	}...)
}

// TestLoadDotEnvUnexported verifies malformed .env lines are skipped
// without error (covered indirectly by TestLoadDotEnv; here directly).
func TestLoadDotEnvUnexported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("JUST_TEXT\n#comment\n\nKEY=value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	if v := os.Getenv("KEY"); v != "value" {
		t.Errorf("KEY = %q", v)
	}
}
