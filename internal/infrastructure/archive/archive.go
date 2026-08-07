// Package archive reads files back from the retention ZIP archives.
// Snapshots store a reference of the form
// "{rel_zip_path}::{filename}", e.g. "snapshots/3/2025-06-15.zip::150322.jpg".
package archive

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lmex89/home-cameras/internal/config"
)

// Reader resolves archived files against the archives directory.
type Reader struct {
	archivesDir string
}

// New builds a reader bound to the config's archives directory.
func New(cfg config.Config) *Reader {
	return &Reader{archivesDir: cfg.ArchivesDir()}
}

// ReadSnapshot extracts a snapshot JPEG given its archive reference.
func (r *Reader) ReadSnapshot(archiveRef string) ([]byte, error) {
	zipRel, filename, ok := strings.Cut(archiveRef, "::")
	if !ok || zipRel == "" || filename == "" {
		return nil, fmt.Errorf("invalid archive reference %q", archiveRef)
	}
	zipAbs := filepath.Join(r.archivesDir, zipRel)
	if _, err := os.Stat(zipAbs); err != nil {
		return nil, fmt.Errorf("archive not found: %s", zipAbs)
	}
	return readZipEntry(zipAbs, filename)
}

// ReadVideo extracts an MP4 from the videos archive by filename.
// Derives the archive path from the filename pattern
// "timelapse_{camera_id}_{date}*.mp4".
func (r *Reader) ReadVideo(filename string) ([]byte, error) {
	parts := strings.Split(strings.TrimSuffix(filename, filepath.Ext(filename)), "_")
	if len(parts) < 3 {
		return nil, fmt.Errorf("unrecognized video filename %q", filename)
	}
	camID, datePart := parts[1], parts[2]
	zipAbs := filepath.Join(r.archivesDir, "videos", camID, datePart+".zip")
	if _, err := os.Stat(zipAbs); err != nil {
		return nil, fmt.Errorf("archive not found: %s", zipAbs)
	}
	return readZipEntry(zipAbs, filename)
}

func readZipEntry(zipPath, name string) ([]byte, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, rc); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return nil, fmt.Errorf("%s not found in %s", name, zipPath)
}
