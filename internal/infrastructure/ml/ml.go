// Package ml defines the object-detection abstraction used by the
// analysis service. Multiple backends implement Detector: a stub that
// returns no detections (when the model is missing) and a gocv ONNX
// engine compiled behind the "opencv" build tag.
package ml

import (
	"context"

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

// NewDetector builds the best available detector for the config. When
// compiled without the "opencv" tag (or the model file is absent) a
// stub detector is returned so the application keeps working.
func NewDetector(cfg config.Config) Detector {
	return newGOCVDetector(cfg)
}
