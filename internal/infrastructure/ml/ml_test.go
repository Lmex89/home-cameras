package ml

import (
	"context"
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
