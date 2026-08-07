// Package config loads application configuration from environment
// variables (with a .env file fallback) and exposes strongly-typed
// settings plus derived filesystem paths.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caarlos0/env/v11"
)

// Config holds every runtime setting of the camera monitor. Fields are
// populated from environment variables (optionally seeded from a .env
// file in the working directory) using struct tags.
type Config struct {
	AppName                 string  `env:"APP_NAME" envDefault:"Camera Monitor"`
	Debug                   bool    `env:"DEBUG" envDefault:"true"`
	Host                    string  `env:"HOST" envDefault:"0.0.0.0"`
	Port                    int     `env:"PORT" envDefault:"8004"`
	Timezone                string  `env:"TIMEZONE" envDefault:"America/Mexico_City"`
	SnapshotRetentionDays   int     `env:"SNAPSHOT_RETENTION_DAYS" envDefault:"30"`
	SnapshotZipAfterDays    int     `env:"SNAPSHOT_ZIP_AFTER_DAYS" envDefault:"7"`
	VideoRetentionDays      int     `env:"VIDEO_RETENTION_DAYS" envDefault:"30"`
	DefaultIntervalSeconds  int     `env:"DEFAULT_INTERVAL_SECONDS" envDefault:"10"`
	CaptureTimeoutSeconds   int     `env:"CAPTURE_TIMEOUT_SECONDS" envDefault:"120"`
	HealthCheckIntervalMin  int     `env:"HEALTH_CHECK_INTERVAL_MINUTES" envDefault:"10"`
	YoloModelPath           string  `env:"YOLO_MODEL_PATH" envDefault:"models/yolov8n.pt"`
	YoloConfidenceThreshold float64 `env:"YOLO_CONFIDENCE_THRESHOLD" envDefault:"0.5"`
	ReviewPersonAfterHour   int     `env:"REVIEW_PERSON_AFTER_HOUR" envDefault:"22"`
	ReviewPersonBeforeHour  int     `env:"REVIEW_PERSON_BEFORE_HOUR" envDefault:"6"`
	ReviewMaxPersonCount    int     `env:"REVIEW_MAX_PERSON_COUNT" envDefault:"5"`
	AnalysisEnabled         bool    `env:"ANALYSIS_ENABLED" envDefault:"true"`
	AnalysisIntervalSeconds int     `env:"ANALYSIS_INTERVAL_SECONDS" envDefault:"30"`
	TimelapseEnabled        bool    `env:"TIMELAPSE_ENABLED" envDefault:"true"`
	TimelapseHour           int     `env:"TIMELAPSE_HOUR" envDefault:"21"`
	TimelapseMinute         int     `env:"TIMELAPSE_MINUTE" envDefault:"0"`
	TimelapseCameraID       int     `env:"TIMELAPSE_CAMERA_ID" envDefault:"6"`
	TimelapseObjectClasses  string  `env:"TIMELAPSE_OBJECT_CLASSES" envDefault:"person,car,motorcycle"`
	TimelapseFrameDuration  float64 `env:"TIMELAPSE_FRAME_DURATION" envDefault:"0.4675"`
	TimelapseWorkers        int     `env:"TIMELAPSE_WORKERS" envDefault:"3"`
	TelegramEnabled         bool    `env:"TELEGRAM_ENABLED" envDefault:"false"`
	TelegramBotToken        string  `env:"TELEGRAM_BOT_TOKEN" envDefault:""`
	TelegramChatID          string  `env:"TELEGRAM_CHAT_ID" envDefault:""`
	StorageEnabled          bool    `env:"STORAGE_ENABLED" envDefault:"false"`
	StorageEndpointURL      string  `env:"STORAGE_ENDPOINT_URL" envDefault:""`
	StorageBucketName       string  `env:"STORAGE_BUCKET_NAME" envDefault:""`
	StorageAccessKey        string  `env:"STORAGE_ACCESS_KEY" envDefault:""`
	StorageSecretKey        string  `env:"STORAGE_SECRET_KEY" envDefault:""`
	StoragePublicURL        string  `env:"STORAGE_PUBLIC_URL" envDefault:""`
	StorageRegion           string  `env:"STORAGE_REGION" envDefault:"us-west-004"`

	DataDir string `env:"DATA_DIR" envDefault:"./data"`
}

// BaseDir returns the working directory the binary was launched from
// (where .env and cameras.yaml live). Run from the project root for
// parity with the legacy app layout.
func (c Config) BaseDir() string {
	return mustAbs(".")
}

// SnapshotsDir returns the directory where raw snapshot images live.
func (c Config) SnapshotsDir() string { return filepath.Join(c.DataDir, "snapshots") }

// ArchivesDir returns the directory where ZIP archives are stored.
func (c Config) ArchivesDir() string { return filepath.Join(c.DataDir, "archives") }

// VideosDir returns the directory where generated videos are stored.
func (c Config) VideosDir() string { return filepath.Join(c.DataDir, "videos") }

// LogsDir returns the directory where rotated log files are written.
func (c Config) LogsDir() string { return filepath.Join(c.DataDir, "logs") }

// DBPath returns the SQLite database file path.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "cameras.db") }

// YAMLPath returns the path of the cameras seed file.
func (c Config) YAMLPath() string { return filepath.Join(c.BaseDir(), "cameras.yaml") }

// TimelapseClassSet returns the configured object classes as a set.
func (c Config) TimelapseClassSet() map[string]bool {
	set := make(map[string]bool)
	for _, s := range strings.Split(c.TimelapseObjectClasses, ",") {
		if s = strings.TrimSpace(s); s != "" {
			set[s] = true
		}
	}
	return set
}

// Load reads the .env file (when present), then parses environment
// variables into a Config struct.
func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse env: %w", err)
	}
	return cfg, nil
}

// loadDotEnv seeds os.Environ from a dotenv-style file. Existing
// environment variables take precedence and are never overridden.
func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(strings.Trim(value, `"'`))
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
	return nil
}

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
