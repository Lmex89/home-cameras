package service

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// TestIntHelpers covers the clamp/min/max helpers table-driven.
func TestIntHelpers(t *testing.T) {
	if got := minInt(1, 2); got != 1 {
		t.Errorf("minInt(1,2) = %d", got)
	}
	if got := minInt(5, 5); got != 5 {
		t.Errorf("minInt(5,5) = %d", got)
	}
	if got := maxInt(1, 2); got != 2 {
		t.Errorf("maxInt(1,2) = %d", got)
	}
	tests := []struct {
		v, lo, hi, want int
	}{
		{0, 1, 10, 1},
		{5, 1, 10, 5},
		{99, 1, 10, 10},
		{3, 3, 3, 3},
	}
	for _, tt := range tests {
		if got := clampInt(tt.v, tt.lo, tt.hi); got != tt.want {
			t.Errorf("clampInt(%d,%d,%d) = %d want %d", tt.v, tt.lo, tt.hi, got, tt.want)
		}
	}
}

// TestDrawFrame verifies annotated frames render with and without
// detections (including out-of-bounds boxes clamped to the image).
func TestDrawFrame(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 60, B: 90, A: 255})
		}
	}
	src := filepath.Join(dir, "in.jpg")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, nil); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	tests := []struct {
		name  string
		dets  []domain.Detection
		class map[string]bool
	}{
		{"no detections", nil, nil},
		{"one detection", []domain.Detection{
			{ClassName: "person", Confidence: 0.95, BBox: []float64{10, 10, 30, 40}},
		}, nil},
		{"out of bounds box clamped", []domain.Detection{
			{ClassName: "car", Confidence: 0.5, BBox: []float64{-50, -50, 500, 500}},
		}, map[string]bool{"person": true}},
		{"ignored class filtered", []domain.Detection{
			{ClassName: "cat", Confidence: 0.8, BBox: []float64{1, 2, 3, 4}},
		}, map[string]bool{"person": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(dir, tt.name+".png")
			if err := drawFrame(src, out, tt.dets, tt.class); err != nil {
				t.Fatalf("drawFrame: %v", err)
			}
			if _, err := os.Stat(out); err != nil {
				t.Fatalf("output missing: %v", err)
			}
		})
	}

	if err := drawFrame(filepath.Join(dir, "missing.jpg"), filepath.Join(dir, "x.png"), nil, nil); err == nil {
		t.Fatal("expected error for missing source image")
	}
}

// TestCopyFileAndSize covers the file helpers.
func TestCopyFileAndSize(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.jpg")
	dst := filepath.Join(dir, "b.jpg")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "hello" {
		t.Fatalf("copied content: %q %v", data, err)
	}
	if err := copyFile(filepath.Join(dir, "nope"), dst); err == nil {
		t.Fatal("expected copy error for missing src")
	}
	if got := fileSizeBytes(src); got != 5 {
		t.Errorf("fileSizeBytes = %d", got)
	}
	if got := fileSizeBytes(filepath.Join(dir, "nope")); got != 0 {
		t.Errorf("fileSizeBytes missing = %d", got)
	}
}

// TestLoadImage verifies JPEG decoding and missing-file errors.
func TestLoadImage(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	path := filepath.Join(dir, "img.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, nil); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	decoded, err := loadImage(path)
	if err != nil {
		t.Fatalf("loadImage: %v", err)
	}
	if b := decoded.Bounds(); b.Dx() != 8 || b.Dy() != 8 {
		t.Fatalf("bounds = %v", b)
	}
	if _, err := loadImage(filepath.Join(dir, "missing.jpg")); err == nil {
		t.Fatal("expected error for missing image")
	}
}

// TestMeasureTextWidth verifies the label width approximation.
func TestMeasureTextWidth(t *testing.T) {
	w := measureTextWidth(labelFont(14), "AB")
	if w <= 0 {
		t.Fatalf("width = %v", w)
	}
}

// TestRenderEmptyErrorPaths verifies render calls fail fast when no
// snapshots exist (no ffmpeg involved).
func TestRenderEmptyErrorPaths(t *testing.T) {
	cfg, db := newTestDB(t)
	ctx := context.Background()
	svc := NewTimelapseService(cfg, db, repository.NewSnapshotRepository(db))

	if _, _, err := svc.RenderAnnotated(ctx, 1, time.Now(), nil); err == nil {
		t.Fatal("expected error for empty annotated render")
	}
	if _, _, err := svc.RenderPlain(ctx, nil, time.Now()); err == nil {
		t.Fatal("expected error for empty plain render")
	}
}
