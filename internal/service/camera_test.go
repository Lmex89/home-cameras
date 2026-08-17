package service

import (
	"context"
	"testing"

	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/infrastructure/onvif"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// TestCameraServiceCRUD exercises the full camera service lifecycle.
func TestCameraServiceCRUD(t *testing.T) {
	cfg, db := newTestDB(t)
	ctx := context.Background()
	svc := NewCameraService(repository.NewCameraRepository(db), onvif.NewFromConfig(cfg))

	if _, err := svc.Get(ctx, 1); err == nil {
		t.Fatal("expected error for missing camera")
	}

	cam, err := svc.Create(ctx, domain.CameraCreate{
		Name: "Front", Host: "10.0.0.5", Port: 80, IntervalSeconds: 60, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if cam.ID == 0 {
		t.Fatal("expected generated id")
	}

	got, err := svc.Get(ctx, cam.ID)
	if err != nil || got.Name != "Front" {
		t.Fatalf("get: %v %+v", err, got)
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}

	// Partial update: name + port only.
	port := 8080
	updated, err := svc.Update(ctx, cam.ID, domain.CameraUpdate{Name: strPtr("Back"), Port: &port})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Back" || updated.Port != 8080 || updated.IntervalSeconds != 60 {
		t.Fatalf("update not applied: %+v", updated)
	}

	// Empty update is a no-op refresh.
	refreshed, err := svc.Update(ctx, cam.ID, domain.CameraUpdate{})
	if err != nil || refreshed.Name != "Back" {
		t.Fatalf("no-op update: %v %+v", err, refreshed)
	}

	deleted, err := svc.Delete(ctx, cam.ID)
	if err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	if deleted, _ := svc.Delete(ctx, cam.ID); deleted {
		t.Fatal("second delete should report false")
	}
}
