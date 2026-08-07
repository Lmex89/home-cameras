package ml

import (
	"context"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/domain"
)

// stubDetector is the graceful fallback when the YOLO model or the
// native engine is unavailable. It reports no detections, mirroring
// the legacy "stub mode" behavior.
type stubDetector struct{}

// newGOCVDetector returns the native engine when built with the opencv
// tag, otherwise the stub. The default (non-tagged) build is stub-only.
func newGOCVDetector(cfg config.Config) Detector {
	return newGOCVEngine(cfg)
}

// Available always reports false for the stub.
func (s stubDetector) Available() bool { return false }

// Detect returns an empty detection list.
func (s stubDetector) Detect(ctx context.Context, imagePath string) ([]domain.Detection, error) {
	return nil, nil
}
