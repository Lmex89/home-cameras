package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lmex89/home-cameras/internal/config"
)

// makeZips creates a snapshot archive and a video archive on disk.
func makeZips(t *testing.T, archivesDir string) {
	t.Helper()
	snapZip := filepath.Join(archivesDir, "snapshots", "1", "2026-08-07.zip")
	if err := os.MkdirAll(filepath.Dir(snapZip), 0o755); err != nil {
		t.Fatal(err)
	}
	writeZip(t, snapZip, map[string]string{"120000.jpg": "jpeg-bytes"})

	videoZip := filepath.Join(archivesDir, "videos", "1", "2026-08-07.zip")
	if err := os.MkdirAll(filepath.Dir(videoZip), 0o755); err != nil {
		t.Fatal(err)
	}
	writeZip(t, videoZip, map[string]string{"timelapse_1_2026-08-07.mp4": "mp4-bytes"})
}

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestReadSnapshot verifies archived snapshot extraction.
func TestReadSnapshot(t *testing.T) {
	archivesDir := filepath.Join(t.TempDir(), "archives")
	makeZips(t, archivesDir)
	r := New(config.Config{DataDir: filepath.Dir(archivesDir)})

	data, err := r.ReadSnapshot("snapshots/1/2026-08-07.zip::120000.jpg")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "jpeg-bytes" {
		t.Fatalf("data = %q", data)
	}

	if _, err := r.ReadSnapshot("malformed-ref"); err == nil {
		t.Fatal("expected error for malformed reference")
	}
	if _, err := r.ReadSnapshot("missing.zip::x.jpg"); err == nil {
		t.Fatal("expected error for missing archive")
	}
	if _, err := r.ReadSnapshot("snapshots/1/2026-08-07.zip::nope.jpg"); err == nil {
		t.Fatal("expected error for missing entry")
	}
}

// TestReadVideo verifies video archive extraction and parsing.
func TestReadVideo(t *testing.T) {
	archivesDir := filepath.Join(t.TempDir(), "archives")
	makeZips(t, archivesDir)
	r := New(config.Config{DataDir: filepath.Dir(archivesDir)})

	data, err := r.ReadVideo("timelapse_1_2026-08-07.mp4")
	if err != nil {
		t.Fatalf("read video: %v", err)
	}
	if string(data) != "mp4-bytes" {
		t.Fatalf("data = %q", data)
	}
	if _, err := r.ReadVideo("short.mp4"); err == nil {
		t.Fatal("expected error for unrecognized filename")
	}
	if _, err := r.ReadVideo("timelapse_1_2026-01-01.mp4"); err == nil {
		t.Fatal("expected error for missing archive")
	}
}
