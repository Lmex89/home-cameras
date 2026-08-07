package service

import (
	"context"
	"testing"

	"github.com/jmoiron/sqlx"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/database"
)

// newTestDB opens a real SQLite database (schema + migrations applied)
// in a temp data dir. Returns the config and the pool; the pool is
// closed when the test ends.
func newTestDB(t *testing.T) (config.Config, *sqlx.DB) {
	t.Helper()
	cfg := config.Config{DataDir: t.TempDir()}
	db, err := database.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return cfg, db
}

// analysisTestCfg returns a config tuned for analysis pipeline tests.
func analysisTestCfg(cfg config.Config) config.Config {
	cfg.AnalysisEnabled = true
	cfg.ReviewMaxPersonCount = 5
	cfg.ReviewPersonAfterHour = 22
	cfg.ReviewPersonBeforeHour = 6
	return cfg
}
