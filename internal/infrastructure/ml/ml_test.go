package ml

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lmex89/home-cameras/internal/config"
)

// TestNewDetectorStub verifies the non-opencv build yields the stub.
func TestNewDetectorStub(t *testing.T) {
	d := NewDetector(config.Config{})
	if d.Available() {
		t.Fatal("stub must report unavailable")
	}
	dets, err := d.Detect(context.Background(), "/nonexistent.jpg")
	if err != nil || dets != nil {
		t.Fatalf("stub detect: %v %v", dets, err)
	}
}

// TestModelPath resolves the configured path against the working dir
// and falls back to the .onnx sibling of a missing .pt file.
func TestModelPath(t *testing.T) {
	dir := t.TempDir()
	t.Run("absolute path found", func(t *testing.T) {
		path := filepath.Join(dir, "yolov8n.onnx")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := config.Config{YoloModelPath: path}
		if got := ModelPath(cfg); got != path {
			t.Fatalf("ModelPath = %q, want %q", got, path)
		}
	})
	t.Run("onnx fallback for missing pt", func(t *testing.T) {
		os.Remove(filepath.Join(dir, "yolov8n.pt"))
		onnx := filepath.Join(dir, "yolov8n.onnx")
		if err := os.WriteFile(onnx, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := config.Config{YoloModelPath: filepath.Join(dir, "yolov8n.pt")}
		if got := ModelPath(cfg); got != onnx {
			t.Fatalf("ModelPath = %q, want %q", got, onnx)
		}
	})
	t.Run("onnx preferred over pt when both exist", func(t *testing.T) {
		onnx := filepath.Join(dir, "yolov8n.onnx")
		pt := filepath.Join(dir, "yolov8n.pt")
		for _, p := range []string{onnx, pt} {
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cfg := config.Config{YoloModelPath: pt}
		if got := ModelPath(cfg); got != onnx {
			t.Fatalf("ModelPath = %q, want %q (gocv cannot read .pt)", got, onnx)
		}
	})
	t.Run("pt alone is unusable", func(t *testing.T) {
		os.Remove(filepath.Join(dir, "yolov8n.onnx"))
		cfg := config.Config{YoloModelPath: filepath.Join(dir, "yolov8n.pt")}
		if got := ModelPath(cfg); got != "" {
			t.Fatalf("ModelPath = %q, want empty for .pt-only", got)
		}
	})
	t.Run("missing returns empty", func(t *testing.T) {
		cfg := config.Config{YoloModelPath: filepath.Join(dir, "nope.pt")}
		if got := ModelPath(cfg); got != "" {
			t.Fatalf("ModelPath = %q, want empty", got)
		}
	})
}
