// Package service contains the application services that orchestrate
// repositories and infrastructure adapters. Each service is constructed
// with injected dependencies (dependency inversion).
package service

import (
	"context"
	"errors"

	"github.com/jmoiron/sqlx"

	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/infrastructure/onvif"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// CameraService coordinates camera CRUD and ONVIF connectivity tests.
type CameraService struct {
	db    *sqlx.DB
	repo  *repository.CameraRepository
	onvif *onvif.Client
}

// NewCameraService builds the camera service with injected dependencies.
func NewCameraService(db *sqlx.DB, repo *repository.CameraRepository, onvifClient *onvif.Client) *CameraService {
	return &CameraService{db: db, repo: repo, onvif: onvifClient}
}

// List returns all cameras.
func (s *CameraService) List(ctx context.Context) ([]domain.Camera, error) {
	return s.repo.GetAll(ctx)
}

// Get returns one camera or an error when missing.
func (s *CameraService) Get(ctx context.Context, id int64) (*domain.Camera, error) {
	return s.repo.GetByID(ctx, id)
}

// Create persists a new camera and returns it with its id.
func (s *CameraService) Create(ctx context.Context, data domain.CameraCreate) (*domain.Camera, error) {
	cam := &domain.Camera{
		Name:            data.Name,
		Host:            data.Host,
		Port:            data.Port,
		Username:        data.Username,
		Password:        data.Password,
		ProfileToken:    data.ProfileToken,
		SnapshotURL:     data.SnapshotURL,
		IntervalSeconds: data.IntervalSeconds,
		Enabled:         data.Enabled,
	}
	if err := s.repo.Add(ctx, cam); err != nil {
		return nil, err
	}
	return cam, nil
}

// Update applies a partial update and returns the refreshed camera.
func (s *CameraService) Update(ctx context.Context, id int64, data domain.CameraUpdate) (*domain.Camera, error) {
	values := map[string]any{}
	if data.Name != nil {
		values["name"] = *data.Name
	}
	if data.Host != nil {
		values["host"] = *data.Host
	}
	if data.Port != nil {
		values["port"] = *data.Port
	}
	if data.Username != nil {
		values["username"] = *data.Username
	}
	if data.Password != nil {
		values["password"] = *data.Password
	}
	if data.ProfileToken != nil {
		values["profile_token"] = *data.ProfileToken
	}
	if data.SnapshotURL != nil {
		values["snapshot_url"] = *data.SnapshotURL
	}
	if data.IntervalSeconds != nil {
		values["interval_seconds"] = *data.IntervalSeconds
	}
	if data.Enabled != nil {
		values["enabled"] = *data.Enabled
	}
	if len(values) == 0 {
		return s.repo.GetByID(ctx, id)
	}
	return s.repo.Update(ctx, id, values)
}

// Delete removes a camera, reporting whether it existed.
func (s *CameraService) Delete(ctx context.Context, id int64) (bool, error) {
	return s.repo.Delete(ctx, id)
}

// Test probes an ONVIF endpoint without persisting anything.
func (s *CameraService) Test(ctx context.Context, host string, port int, user, pass string) (domain.CameraTestResult, error) {
	res := s.onvif.TestConnection(host, port, user, pass)
	tokens := make([]string, 0, len(res.Profiles))
	for _, p := range res.Profiles {
		tokens = append(tokens, p.Token)
	}
	errMsg := &res.Error
	if res.Error == "" {
		errMsg = nil
	}
	return domain.CameraTestResult{
		Reachable: res.Reachable,
		Profiles:  tokens,
		Error:     errMsg,
	}, nil
}

// ErrNotFound is exported for handlers to map to 404 responses.
var ErrNotFound = errors.New("camera not found")
