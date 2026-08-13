// Package seed syncs camera metadata from cameras.yaml into the
// database at startup (idempotent upsert-by-name, deleting cameras that
// were removed from the YAML). Parity with the legacy seed.py.
package seed

import (
	"context"
	"fmt"
	"os"

	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// yamlCamera mirrors one entry of the cameras.yaml "cameras" list.
type yamlCamera struct {
	Name            string  `yaml:"name"`
	CameraType      string  `yaml:"camera_type"`
	Host            string  `yaml:"host"`
	Port            int     `yaml:"port"`
	Username        string  `yaml:"username"`
	Password        string  `yaml:"password"`
	ProfileToken    *string `yaml:"profile_token"`
	SnapshotURL     *string `yaml:"snapshot_url"`
	DevicePath      *string `yaml:"device_path"`
	IntervalSeconds int     `yaml:"interval_seconds"`
	Enabled         bool    `yaml:"enabled"`
}

// yamlFile is the root document of cameras.yaml.
type yamlFile struct {
	Cameras []yamlCamera `yaml:"cameras"`
}

// FromYAML loads cameras from the YAML file at yamlPath and syncs them
// with the database: cameras present in both are updated, new cameras
// are inserted, and cameras missing from the YAML are deleted.
//
// Args:
//
//	ctx: Request context.
//	db: Database pool used for the sync transaction.
//	yamlPath: Absolute path of cameras.yaml.
//	defaultInterval: Fallback interval when the YAML omits it.
//
// Returns:
//
//	True when cameras were seeded/updated, false when the file is
//	missing or empty.
func FromYAML(ctx context.Context, db *sqlx.DB, yamlPath string, defaultInterval int) (bool, error) {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Str("path", yamlPath).Msg("cameras.yaml not found, skipping seed")
			return false, nil
		}
		return false, err
	}
	var doc yamlFile
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, fmt.Errorf("parse cameras.yaml: %w", err)
	}
	if len(doc.Cameras) == 0 {
		log.Warn().Str("path", yamlPath).Msg("no cameras defined in YAML")
		return false, nil
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	repo := repository.NewCameraRepository(tx)

	existing, err := repo.GetAll(ctx)
	if err != nil {
		return false, err
	}
	existingByName := map[string]domain.Camera{}
	for _, c := range existing {
		existingByName[c.Name] = c
	}

	seeded := 0
	for _, raw := range doc.Cameras {
		interval := raw.IntervalSeconds
		if interval == 0 {
			interval = defaultInterval
		}
		cameraType := raw.CameraType
		if cameraType == "" {
			cameraType = "ip"
		}
		values := map[string]any{
			"camera_type":      cameraType,
			"host":             raw.Host,
			"port":             raw.Port,
			"username":         raw.Username,
			"password":         raw.Password,
			"profile_token":    raw.ProfileToken,
			"snapshot_url":     raw.SnapshotURL,
			"device_path":      raw.DevicePath,
			"interval_seconds": interval,
			"enabled":          raw.Enabled,
		}
		if cam, ok := existingByName[raw.Name]; ok {
			if _, err := repo.Update(ctx, cam.ID, values); err != nil {
				return false, err
			}
			log.Info().Str("camera", raw.Name).Msg("seed: updated camera")
		} else {
			newCam := &domain.Camera{
				Name:            raw.Name,
				CameraType:      cameraType,
				Host:            raw.Host,
				Port:            raw.Port,
				Username:        raw.Username,
				Password:        raw.Password,
				ProfileToken:    raw.ProfileToken,
				SnapshotURL:     raw.SnapshotURL,
				DevicePath:      raw.DevicePath,
				IntervalSeconds: interval,
				Enabled:         raw.Enabled,
			}
			if err := repo.Add(ctx, newCam); err != nil {
				return false, err
			}
			log.Info().Str("camera", raw.Name).Msg("seed: inserted camera")
		}
		seeded++
	}

	// Delete cameras that were removed from the YAML.
	yamlNames := map[string]bool{}
	for _, raw := range doc.Cameras {
		yamlNames[raw.Name] = true
	}
	for name, cam := range existingByName {
		if !yamlNames[name] {
			if _, err := repo.Delete(ctx, cam.ID); err != nil {
				return false, err
			}
			log.Info().Str("camera", name).Msg("seed: removed camera not in YAML")
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	log.Info().Int("cameras", seeded).Msg("seed complete")
	return true, nil
}
