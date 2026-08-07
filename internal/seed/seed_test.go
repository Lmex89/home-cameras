package seed

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// openSeedDB opens an in-memory SQLite database with the cameras table.
func openSeedDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
CREATE TABLE cameras (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL, host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 80,
    username TEXT NOT NULL DEFAULT '',
    password TEXT NOT NULL DEFAULT '',
    profile_token TEXT, snapshot_url TEXT,
    interval_seconds INTEGER NOT NULL DEFAULT 60,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);`); err != nil {
		t.Fatal(err)
	}
	return db
}

const yamlDoc = `
cameras:
  - name: Front
    host: 10.0.0.5
    port: 80
    username: admin
    password: secret
    interval_seconds: 45
    enabled: true
  - name: Back
    host: 10.0.0.6
    port: 8080
    enabled: false
`

// TestFromYAMLUpsert verifies insert, update and delete sync behavior.
func TestFromYAMLUpsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cameras.yaml")
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	db := openSeedDB(t)
	ctx := context.Background()
	repo := repository.NewCameraRepository(db)

	// Pre-existing camera named Back with a different host (will update)
	// plus an extra camera not in YAML (will delete).
	if err := repo.Add(ctx, &domain.Camera{Name: "Back", Host: "10.0.0.99", Port: 1}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Add(ctx, &domain.Camera{Name: "OldCam", Host: "10.0.0.7", Port: 80}); err != nil {
		t.Fatal(err)
	}

	seeded, err := FromYAML(ctx, db, path, 60)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !seeded {
		t.Fatal("expected seeding to happen")
	}

	cams, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 2 {
		t.Fatalf("camera count = %d want 2", len(cams))
	}
	byName := map[string]domain.Camera{}
	for _, c := range cams {
		byName[c.Name] = c
	}
	front := byName["Front"]
	if front.Host != "10.0.0.5" || front.IntervalSeconds != 45 || front.Username != "admin" {
		t.Fatalf("front = %+v", front)
	}
	back := byName["Back"]
	if back.Host != "10.0.0.6" || back.Port != 8080 || back.Enabled {
		t.Fatalf("back not updated: %+v", back)
	}
	if _, ok := byName["OldCam"]; ok {
		t.Fatal("OldCam should have been deleted")
	}
}

// TestFromYAMLDefaultInterval verifies omitted intervals use the default.
func TestFromYAMLDefaultInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cameras.yaml")
	if err := os.WriteFile(path, []byte(`
cameras:
  - name: NoInterval
    host: 10.0.0.5
`), 0o644); err != nil {
		t.Fatal(err)
	}
	db := openSeedDB(t)
	seeded, err := FromYAML(context.Background(), db, path, 123)
	if err != nil || !seeded {
		t.Fatalf("seed: %v %v", seeded, err)
	}
	cams, err := repository.NewCameraRepository(db).GetAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 1 || cams[0].IntervalSeconds != 123 {
		t.Fatalf("cams = %+v", cams)
	}
}

// TestFromYAMLEdgeCases covers missing/empty/invalid files.
func TestFromYAMLEdgeCases(t *testing.T) {
	db := openSeedDB(t)
	ctx := context.Background()

	// Missing file -> false, nil.
	seeded, err := FromYAML(ctx, db, filepath.Join(t.TempDir(), "nope.yaml"), 60)
	if err != nil || seeded {
		t.Fatalf("missing: %v %v", seeded, err)
	}

	// Empty cameras list -> false, nil.
	emptyPath := filepath.Join(t.TempDir(), "empty.yaml")
	if err := os.WriteFile(emptyPath, []byte("cameras: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seeded, err = FromYAML(ctx, db, emptyPath, 60)
	if err != nil || seeded {
		t.Fatalf("empty: %v %v", seeded, err)
	}

	// Invalid YAML -> error.
	badPath := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(badPath, []byte("cameras: [{{{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FromYAML(ctx, db, badPath, 60); err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
