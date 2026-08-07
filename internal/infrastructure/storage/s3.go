// Package storage uploads files to an S3-compatible object store
// (Backblaze B2, MinIO, AWS) using the minio SDK.
package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/Lmex89/home-cameras/internal/config"
)

// S3 is an S3-compatible uploader bound to one bucket.
type S3 struct {
	client    *minio.Client
	bucket    string
	publicURL string
}

// New builds an S3 client from explicit parameters.
func New(endpoint, bucket, accessKey, secretKey, region string) (*S3, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: strings.HasPrefix(endpoint, "https"),
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	return &S3{client: client, bucket: bucket}, nil
}

// NewFromConfig builds an S3 client from the application config.
// Returns an error when storage is disabled or misconfigured.
func NewFromConfig(cfg config.Config) (*S3, error) {
	if !cfg.StorageEnabled {
		return nil, fmt.Errorf("storage disabled")
	}
	if cfg.StorageEndpointURL == "" || cfg.StorageBucketName == "" ||
		cfg.StorageAccessKey == "" || cfg.StorageSecretKey == "" {
		return nil, fmt.Errorf("storage misconfigured: missing endpoint/bucket/keys")
	}
	s, err := New(cfg.StorageEndpointURL, cfg.StorageBucketName,
		cfg.StorageAccessKey, cfg.StorageSecretKey, cfg.StorageRegion)
	if err != nil {
		return nil, err
	}
	s.publicURL = strings.TrimRight(cfg.StoragePublicURL, "/")
	return s, nil
}

// Upload pushes a file under videos/ and returns its public URL.
func (s *S3) Upload(ctx context.Context, localPath string) (string, error) {
	key := "videos/" + filepath.Base(localPath)
	if _, err := s.client.FPutObject(ctx, s.bucket, key, localPath, minio.PutObjectOptions{
		ContentType: "video/mp4",
	}); err != nil {
		return "", fmt.Errorf("upload %s: %w", key, err)
	}
	if s.publicURL == "" {
		return "", nil
	}
	return s.publicURL + "/" + key, nil
}

// Enabled reports whether storage is configured (used by callers that
// want to skip upload attempts cheaply).
func (s *S3) Enabled() bool { return s != nil }

// SizeMB returns a file size in megabytes.
func SizeMB(path string) float64 {
	if info, err := os.Stat(path); err == nil {
		return float64(info.Size()) / (1024 * 1024)
	}
	return 0
}
