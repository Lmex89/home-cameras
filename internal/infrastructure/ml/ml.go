// Package ml defines the object-detection abstraction used by the
// analysis service. Multiple backends implement Detector: a stub that
// returns no detections (when the model is missing) and a gocv ONNX
// engine compiled behind the "opencv" build tag.
package ml

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/domain"
)

// Detector runs object detection over a snapshot image.
type Detector interface {
	// Available reports whether the model is loaded and usable.
	Available() bool
	// Detect runs inference and returns detections (empty on failure).
	Detect(ctx context.Context, imagePath string) ([]domain.Detection, error)
}

// ModelPath resolves the configured YOLO_MODEL_PATH against the working
// directory. gocv cannot read PyTorch .pt weights, so a sibling .onnx
// export is preferred whenever the configured path points at a .pt (or
// the file itself is missing). Returns "" when no usable model exists.
//
// Args:
//
//	cfg: Application config.
//
// Returns:
//
//	The resolved model path, or "" when no usable model file is present.
func ModelPath(cfg config.Config) string {
	p := cfg.YoloModelPath
	if !filepath.IsAbs(p) {
		p = filepath.Join(cfg.BaseDir(), p)
	}
	ext := filepath.Ext(p)
	base := p[:len(p)-len(ext)]

	if ext == ".pt" {
		if _, err := os.Stat(base + ".onnx"); err == nil {
			return base + ".onnx"
		}
		return ""
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if _, err := os.Stat(base + ".onnx"); err == nil {
		return base + ".onnx"
	}
	return ""
}

// NewDetector builds the best available detector for the config. When
// compiled without the "opencv" tag (or the model file is absent) a
// stub detector is returned so the application keeps working.
func NewDetector(cfg config.Config) Detector {
	return newGOCVDetector(cfg)
}
