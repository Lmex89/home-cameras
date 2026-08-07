package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHelperFuncs covers the small pure helpers.
func TestHelperFuncs(t *testing.T) {
	if got := itosa(42); got != "42" {
		t.Errorf("itosa = %q", got)
	}
	if got := itoa(7); got != "7" {
		t.Errorf("itoa = %q", got)
	}
	h := 9
	if got := timeString(&h); got != "9" {
		t.Errorf("timeString = %q", got)
	}
	if got := timeString(nil); got != "" {
		t.Errorf("timeString nil = %q", got)
	}
	if got := strPtr("x"); got == nil || *got != "x" {
		t.Errorf("strPtr = %v", got)
	}
	if got := strPtr(""); got != nil {
		t.Errorf("strPtr empty = %v", got)
	}
	if n, err := parseInt("42"); err != nil || n != 42 {
		t.Errorf("parseInt = %v %v", n, err)
	}
	if n, err := parseInt64("42"); err != nil || n != 42 {
		t.Errorf("parseInt64 = %v %v", n, err)
	}
	if n, err := parseInt("abc"); err == nil {
		t.Errorf("parseInt bad = %v", n)
	}
	if n, err := parseInt64("abc"); err == nil {
		t.Errorf("parseInt64 bad = %v", n)
	}
}

// TestMoveFile covers rename and cross-device copy fallback.
func TestMoveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("move: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "data" {
		t.Fatalf("moved content: %q %v", data, err)
	}
	// Missing source falls back to copy -> read error surfaces.
	if err := moveFile(filepath.Join(dir, "nope"), filepath.Join(dir, "c")); err == nil {
		t.Fatal("expected error for missing source")
	}
}

// TestFileSizeMB covers the size formatter.
func TestFileSizeMB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, make([]byte, 2*1024*1024+1024*512), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fileSizeMB(path); got != "2.5" {
		t.Errorf("fileSizeMB = %q", got)
	}
	if got := fileSizeMB(filepath.Join(t.TempDir(), "nope")); got != "0.0" {
		t.Errorf("fileSizeMB missing = %q", got)
	}
}

// TestServerShutdown verifies the (currently empty) drain hook.
func TestServerShutdown(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

// TestSnapshotFilesHandler covers the raw snapshot file mount.
func TestSnapshotFilesHandler(t *testing.T) {
	s, cfg := newTestServer(t)
	rel := filepath.Join("1", "2026", "08", "07", "120000.jpg")
	full := filepath.Join(cfg.SnapshotsDir(), rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := do(s, http.MethodGet, "/snapshots/"+filepath.ToSlash(rel), "")
	if w.Code != http.StatusOK {
		t.Fatalf("served: %d", w.Code)
	}
	w = do(s, http.MethodGet, "/snapshots/1/2026/01/01/000000.jpg", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", w.Code)
	}
	w = do(s, http.MethodGet, "/snapshots/../../secret", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("traversal: %d", w.Code)
	}
}

// TestRequestTimeout verifies the bounded context helper.
func TestRequestTimeout(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	ctx, cancel := requestTimeout(r, time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		t.Fatalf("fresh ctx: %v", err)
	}
}
