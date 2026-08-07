package ml

import (
	"github.com/Lmex89/home-cameras/internal/config"
)

// newGOCVEngine is the default (non-OpenCV) build: the gocv engine is
// unavailable, so a stub detector is returned.
func newGOCVEngine(cfg config.Config) Detector {
	return stubDetector{}
}
