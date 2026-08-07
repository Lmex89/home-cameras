package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lmex89/home-cameras/internal/config"
)

// TestNewFromConfigValidation covers disabled/misconfigured paths.
func TestNewFromConfigValidation(t *testing.T) {
	if _, err := NewFromConfig(config.Config{StorageEnabled: false}); err == nil {
		t.Fatal("expected error when storage disabled")
	}
	if _, err := NewFromConfig(config.Config{
		StorageEnabled: true,
	}); err == nil {
		t.Fatal("expected error when missing endpoint/bucket/keys")
	}
}

// TestSizeMB verifies file sizing with a fallback of zero.
func TestSizeMB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(path, make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := SizeMB(path); got != 2.0 {
		t.Errorf("SizeMB = %v", got)
	}
	if got := SizeMB(filepath.Join(dir, "nope")); got != 0 {
		t.Errorf("SizeMB missing = %v", got)
	}
}

// TestNewBadEndpoint verifies an invalid endpoint surfaces an error.
func TestNewBadEndpoint(t *testing.T) {
	if _, err := New("", "bucket", "ak", "sk", "region"); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

// TestUploadToFakeS3 verifies FPutObject against a minimal S3 stand-in.
func TestUploadToFakeS3(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.Copy(io.Discard, r.Body) // drain to avoid client EOF
		// minio preflights bucket location; answer with valid XML.
		if strings.Contains(r.URL.RawQuery, "location") {
			io.WriteString(w, `<?xml version="1.0"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`)
			return
		}
		// A single PUT is enough for small files; ETag makes minio happy.
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s, err := New(strings.TrimPrefix(srv.URL, "http://"), "test-bucket", "ak", "sk", "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	s.publicURL = "https://public.example"

	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(path, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}

	url, err := s.Upload(context.Background(), path)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if gotPath != "/test-bucket/videos/clip.mp4" {
		t.Fatalf("path = %q", gotPath)
	}
	if url != "https://public.example/videos/clip.mp4" {
		t.Fatalf("url = %q", url)
	}
}

// TestNewFromConfigSuccess verifies the success path builds a client.
func TestNewFromConfigSuccess(t *testing.T) {
	s, err := NewFromConfig(config.Config{
		StorageEnabled:     true,
		StorageEndpointURL: "localhost:9000",
		StorageBucketName:  "bkt",
		StorageAccessKey:   "ak",
		StorageSecretKey:   "sk",
		StoragePublicURL:   "https://pub.example/",
		StorageRegion:      "eu",
	})
	if err != nil {
		t.Fatalf("new from config: %v", err)
	}
	if s.publicURL != "https://pub.example" {
		t.Fatalf("public url = %q", s.publicURL)
	}
}

// TestUploadNoPublicURL verifies a missing public URL yields an empty
// upload result.
func TestUploadNoPublicURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if strings.Contains(r.URL.RawQuery, "location") {
			io.WriteString(w, `<?xml version="1.0"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`)
			return
		}
		w.Header().Set("ETag", `"x"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s, err := New(strings.TrimPrefix(srv.URL, "http://"), "bkt", "ak", "sk", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "f.mp4")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	url, err := s.Upload(context.Background(), path)
	if err != nil || url != "" {
		t.Fatalf("upload: %q %v", url, err)
	}
}

// TestEnabled covers the nil-safe enabled predicate.
func TestEnabled(t *testing.T) {
	var nilS3 *S3
	if nilS3.Enabled() {
		t.Fatal("nil S3 must report disabled")
	}
	if !(&S3{}).Enabled() {
		t.Fatal("non-nil S3 must report enabled")
	}
}
