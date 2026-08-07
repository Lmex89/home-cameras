package service

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// RetentionService manages the filesystem + database lifecycle: zipping
// old snapshots/videos into archives, deleting records past the
// retention cutoff, and the destructive purge path. Callers (cron job
// or API endpoint) are expected to pause capture/analysis jobs while
// running to avoid SQLite write contention.
type RetentionService struct {
	cfg      config.Config
	db       *sqlx.DB
	snaps    *repository.SnapshotRepository
	analyses *repository.SnapshotAnalysisRepository
	jobs     *repository.AnalysisJobRepository
}

// NewRetentionService builds the retention service.
//
// Args:
//
//	cfg: Application configuration (thresholds, dirs).
//	db: Shared database pool.
//
// Returns:
//
//	A ready RetentionService.
func NewRetentionService(cfg config.Config, db *sqlx.DB) *RetentionService {
	return &RetentionService{
		cfg:      cfg,
		db:       db,
		snaps:    repository.NewSnapshotRepository(db),
		analyses: repository.NewSnapshotAnalysisRepository(db),
		jobs:     repository.NewAnalysisJobRepository(db),
	}
}

// Run executes all retention steps in order and returns per-step counts.
//
// Args:
//
//	ctx: Request context.
//
// Returns:
//
//	A map of step name -> count (snapshots_zipped, snapshots_deleted,
//	videos_archived, videos_deleted).
func (s *RetentionService) Run(ctx context.Context) (map[string]int64, error) {
	now := time.Now()
	zipCutoff := now.AddDate(0, 0, -s.cfg.SnapshotZipAfterDays)
	deleteCutoff := now.AddDate(0, 0, -s.cfg.SnapshotRetentionDays)
	videoCutoff := now.AddDate(0, 0, -s.cfg.VideoRetentionDays)

	result := map[string]int64{}
	var err error

	if result["snapshots_zipped"], err = s.zipOldSnapshots(ctx, zipCutoff); err != nil {
		return nil, err
	}
	if result["snapshots_deleted"], err = s.deleteExpiredSnapshots(ctx, deleteCutoff); err != nil {
		return nil, err
	}
	if result["videos_archived"], err = s.archiveOldVideos(ctx, zipCutoff); err != nil {
		return nil, err
	}
	if result["videos_deleted"], err = s.deleteExpiredVideos(ctx, videoCutoff); err != nil {
		return nil, err
	}

	total := result["snapshots_zipped"] + result["snapshots_deleted"] +
		result["videos_archived"] + result["videos_deleted"]
	if total > 0 {
		log.Info().Interface("result", result).Msg("retention cleanup complete")
	}
	return result, nil
}

// PurgeOlderThan permanently deletes snapshots, analyses, jobs, videos
// and archives older than days — WITHOUT creating archives. Destructive.
//
// Args:
//
//	ctx: Request context.
//	days: Number of days to keep; anything older is deleted.
//
// Returns:
//
//	A map of step name -> count of deleted items.
func (s *RetentionService) PurgeOlderThan(ctx context.Context, days int) (map[string]int64, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	log.Warn().Int("days", days).Time("cutoff", cutoff).Msg("PURGE: deleting snapshots, analyses, videos and archives")

	result := map[string]int64{}

	// Fetch snapshot ids/paths first so raw files can be deleted before
	// the DB rows (parity with the legacy purge flow).
	rows, err := s.db.QueryxContext(ctx,
		"SELECT id, image_path, archive_path FROM snapshots WHERE captured_at < ?",
		cutoff.Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, err
	}
	type purgeRow struct {
		ID          int64   `db:"id"`
		ImagePath   string  `db:"image_path"`
		ArchivePath *string `db:"archive_path"`
	}
	var snapshots []purgeRow
	for rows.Next() {
		var r purgeRow
		if err := rows.StructScan(&r); err != nil {
			rows.Close()
			return nil, err
		}
		snapshots = append(snapshots, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Delete raw image files.
	rawDeleted := int64(0)
	ids := make([]int64, 0, len(snapshots))
	for _, r := range snapshots {
		ids = append(ids, r.ID)
		if r.ImagePath == "" {
			continue
		}
		fullPath := filepath.Join(s.cfg.SnapshotsDir(), r.ImagePath)
		if err := os.Remove(fullPath); err == nil {
			rawDeleted++
		}
	}
	result["raw_snapshots_deleted"] = rawDeleted

	// Belt-and-suspenders: delete child records first, then snapshots.
	if result["snapshot_analyses_deleted"], err = s.analyses.DeleteBySnapshotIDs(ctx, ids); err != nil {
		return nil, err
	}
	if result["analysis_jobs_deleted"], err = s.jobs.DeleteBySnapshotIDs(ctx, ids); err != nil {
		return nil, err
	}
	if result["snapshots_deleted"], err = s.snaps.DeleteOlderThan(ctx, cutoff); err != nil {
		return nil, err
	}

	// Delete orphaned snapshot archive ZIPs.
	if result["snapshot_archives_deleted"], err = s.deleteOrphanedZips(ctx, "snapshots"); err != nil {
		return nil, err
	}

	// Delete videos and video archives.
	if result["videos_deleted"], err = s.deleteExpiredVideos(ctx, cutoff); err != nil {
		return nil, err
	}
	if result["video_archives_deleted"], err = s.deleteExpiredVideoArchives(ctx, cutoff); err != nil {
		return nil, err
	}

	log.Warn().Interface("result", result).Msg("PURGE complete")
	return result, nil
}

// zipOldSnapshots compresses snapshots older than cutoff into daily ZIP
// archives under data/archives/snapshots/{camera_id}/{date}.zip. Each
// snapshot row gains an archive_path reference and the raw JPG is
// deleted. Archive writes go to a temp file then rename (atomic), and
// DB updates run inside a per-group transaction.
//
// Args:
//
//	ctx: Request context.
//	cutoff: Snapshots older than this are zipped.
//
// Returns:
//
//	The number of snapshots archived.
func (s *RetentionService) zipOldSnapshots(ctx context.Context, cutoff time.Time) (int64, error) {
	snapshots, err := s.snaps.GetOldUnarchived(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	if len(snapshots) == 0 {
		return 0, nil
	}
	log.Info().Int("snapshots", len(snapshots)).Msg("archiving old snapshots")

	// Group by (camera_id, date) preserving capture order.
	type snapInfo struct {
		id   int64
		path string
	}
	groups := map[[2]any][]snapInfo{}
	for _, sn := range snapshots {
		key := [2]any{sn.CameraID, sn.CapturedAt.Format("2006-01-02")}
		groups[key] = append(groups[key], snapInfo{id: sn.ID, path: sn.ImagePath})
	}

	archived := int64(0)
	base := filepath.Join(s.cfg.ArchivesDir(), "snapshots")
	for key, group := range groups {
		camID := key[0].(int64)
		date := key[1].(string)
		zipRel := fmt.Sprintf("snapshots/%d/%s.zip", camID, date)
		zipAbs := filepath.Join(base, fmt.Sprintf("%d", camID), date+".zip")
		if err := os.MkdirAll(filepath.Dir(zipAbs), 0o755); err != nil {
			return archived, err
		}

		existing := zipEntryNames(zipAbs)

		tx, err := s.db.BeginTxx(ctx, nil)
		if err != nil {
			return archived, err
		}
		txSnaps := repository.NewSnapshotRepository(tx)

		for _, info := range group {
			name := filepath.Base(info.path)
			ref := fmt.Sprintf("%s::%s", zipRel, name)

			if _, ok := existing[name]; ok {
				// Already archived (e.g. after a rolled-back tx).
				if err := txSnaps.UpdateArchivePath(ctx, info.id, ref); err == nil {
					archived++
				}
				continue
			}
			src := filepath.Join(s.cfg.SnapshotsDir(), info.path)
			if _, err := os.Stat(src); err != nil {
				continue // raw file already gone
			}
			if err := appendToZip(zipAbs, src, name); err != nil {
				log.Error().Err(err).Str("zip", zipAbs).Str("file", src).Msg("zip write failed")
				continue
			}
			if err := txSnaps.UpdateArchivePath(ctx, info.id, ref); err != nil {
				log.Error().Err(err).Int64("snapshot_id", info.id).Msg("archive_path update failed")
				continue
			}
			os.Remove(src)
			archived++
		}
		if err := tx.Commit(); err != nil {
			return archived, err
		}
	}
	log.Info().Int64("archived", archived).Int("groups", len(groups)).Msg("snapshot zipping done")
	return archived, nil
}

// deleteExpiredSnapshots deletes snapshot rows past the retention cutoff
// and removes orphaned archive ZIPs (zips whose every snapshot is gone).
//
// Args:
//
//	ctx: Request context.
//	cutoff: Snapshots older than this are deleted.
//
// Returns:
//
//	The number of rows deleted.
func (s *RetentionService) deleteExpiredSnapshots(ctx context.Context, cutoff time.Time) (int64, error) {
	deleted, err := s.snaps.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	if n, err := s.deleteOrphanedZips(ctx, "snapshots"); err != nil {
		log.Warn().Err(err).Msg("orphaned zip cleanup failed")
	} else if n > 0 {
		log.Debug().Int64("zips", n).Msg("deleted orphaned snapshot archives")
	}
	if deleted > 0 {
		log.Info().Int64("deleted", deleted).Msg("deleted expired snapshot records")
	}
	return deleted, nil
}

// deleteOrphanedZips removes ZIPs under archives/{kind} that no snapshot
// references anymore.
//
// Args:
//
//	ctx: Request context.
//	kind: Subdirectory ("snapshots" or "videos").
//
// Returns:
//
//	The number of ZIP files deleted.
func (s *RetentionService) deleteOrphanedZips(ctx context.Context, kind string) (int64, error) {
	base := filepath.Join(s.cfg.ArchivesDir(), kind)
	if _, err := os.Stat(base); err != nil {
		return 0, nil
	}
	deleted := int64(0)
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(path, ".zip") {
			return nil
		}
		rel, err := filepath.Rel(s.cfg.ArchivesDir(), path)
		if err != nil {
			return nil
		}
		count, err := s.snaps.CountByArchiveZip(ctx, rel)
		if err != nil {
			return nil
		}
		if count == 0 {
			if err := os.Remove(path); err == nil {
				deleted++
				log.Debug().Str("zip", path).Msg("deleted orphaned archive")
			}
		}
		return nil
	})
	return deleted, err
}

// archiveOldVideos compresses videos older than cutoff into daily ZIPs
// under data/archives/videos/{camera_id}/{date}.zip. Camera id and date
// are parsed from the filename pattern timelapse_{id}_{date}*.mp4.
//
// Args:
//
//	ctx: Request context.
//	cutoff: Videos older than this are archived.
//
// Returns:
//
//	The number of videos archived.
func (s *RetentionService) archiveOldVideos(ctx context.Context, cutoff time.Time) (int64, error) {
	videosDir := s.cfg.VideosDir()
	if _, err := os.Stat(videosDir); err != nil {
		return 0, nil
	}
	entries, err := os.ReadDir(videosDir)
	if err != nil {
		return 0, err
	}
	archived := int64(0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mp4") {
			continue
		}
		full := filepath.Join(videosDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(entry.Name(), ".mp4"), "_")
		camID, date := "0", info.ModTime().Format("2006-01-02")
		if len(parts) > 1 {
			camID = parts[1]
		}
		if len(parts) > 2 {
			date = parts[2]
		}
		zipAbs := filepath.Join(s.cfg.ArchivesDir(), "videos", camID, date+".zip")
		if err := os.MkdirAll(filepath.Dir(zipAbs), 0o755); err != nil {
			return archived, err
		}
		if err := appendToZip(zipAbs, full, entry.Name()); err != nil {
			log.Error().Err(err).Str("video", full).Msg("video zip failed")
			continue
		}
		if err := os.Remove(full); err == nil {
			archived++
		}
	}
	if archived > 0 {
		log.Info().Int64("archived", archived).Msg("videos archived into ZIPs")
	}
	return archived, nil
}

// deleteExpiredVideos removes video archive ZIPs and stray MP4s older
// than cutoff (filesystem mtime based, like the legacy code).
//
// Args:
//
//	ctx: Request context.
//	cutoff: Files older than this are deleted.
//
// Returns:
//
//	The number of files deleted.
func (s *RetentionService) deleteExpiredVideos(ctx context.Context, cutoff time.Time) (int64, error) {
	deleted, err := deleteFilesOlderThan(filepath.Join(s.cfg.ArchivesDir(), "videos"), cutoff, ".zip")
	if err != nil {
		return 0, err
	}
	n, err := deleteFilesOlderThan(s.cfg.VideosDir(), cutoff, ".mp4")
	deleted += n
	if deleted > 0 {
		log.Info().Int64("deleted", deleted).Msg("deleted expired video files")
	}
	return deleted, err
}

// deleteExpiredVideoArchives removes only video ZIPs older than cutoff.
//
// Args:
//
//	ctx: Request context.
//	cutoff: Archives older than this are deleted.
//
// Returns:
//
//	The number of ZIP files deleted.
func (s *RetentionService) deleteExpiredVideoArchives(ctx context.Context, cutoff time.Time) (int64, error) {
	deleted, err := deleteFilesOlderThan(filepath.Join(s.cfg.ArchivesDir(), "videos"), cutoff, ".zip")
	if deleted > 0 {
		log.Info().Int64("deleted", deleted).Msg("deleted expired video archives")
	}
	return deleted, err
}

// deleteFilesOlderThan recursively removes files with the given suffix
// whose mtime is older than cutoff.
//
// Args:
//
//	dir: The directory to scan (skipped when missing).
//	cutoff: Older files are removed.
//	suffix: File extension filter (e.g. ".zip").
//
// Returns:
//
//	The number of files deleted.
func deleteFilesOlderThan(dir string, cutoff time.Time, suffix string) (int64, error) {
	if _, err := os.Stat(dir); err != nil {
		return 0, nil
	}
	deleted := int64(0)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, suffix) {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err == nil {
				deleted++
			}
		}
		return nil
	})
	return deleted, err
}

// zipEntryNames lists the entry names of an existing ZIP (empty when
// the file does not exist yet).
//
// Args:
//
//	zipPath: Absolute path of the archive.
//
// Returns:
//
//	A set of entry names.
func zipEntryNames(zipPath string) map[string]bool {
	names := map[string]bool{}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return names
	}
	defer zr.Close()
	for _, f := range zr.File {
		names[f.Name] = true
	}
	return names
}

// appendToZip adds src into zipPath under the given name, preserving
// any existing entries. The archive is rebuilt atomically via a temp
// file + rename so concurrent readers never see a partial archive.
//
// Args:
//
//	zipPath: Absolute path of the archive (created when missing).
//	src: The file to add.
//	name: The entry name inside the archive.
//
// Returns:
//
//	Any I/O error.
func appendToZip(zipPath, src, name string) error {
	tmp := zipPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)

	// Copy existing entries so appends behave like zipfile "a" mode.
	zr, zerr := zip.OpenReader(zipPath)
	if zerr == nil {
		for _, f := range zr.File {
			if f.Name == name {
				continue // dedupe: skip the entry being re-added
			}
			rc, err := f.Open()
			if err != nil {
				zr.Close()
				zw.Close()
				out.Close()
				os.Remove(tmp)
				return err
			}
			hdr := &zip.FileHeader{Name: f.Name, Method: zip.Deflate}
			hdr.SetModTime(f.Modified)
			w, err := zw.CreateHeader(hdr)
			if err != nil {
				rc.Close()
				zr.Close()
				zw.Close()
				out.Close()
				os.Remove(tmp)
				return err
			}
			if _, err := io.Copy(w, rc); err != nil {
				rc.Close()
				zr.Close()
				zw.Close()
				out.Close()
				os.Remove(tmp)
				return err
			}
			rc.Close()
		}
		zr.Close()
	}

	// Add the new file.
	in, err := os.Open(src)
	if err != nil {
		zw.Close()
		out.Close()
		os.Remove(tmp)
		return err
	}
	w, err := zw.Create(name)
	if err != nil {
		in.Close()
		zw.Close()
		out.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := io.Copy(w, in); err != nil {
		in.Close()
		zw.Close()
		out.Close()
		os.Remove(tmp)
		return err
	}
	in.Close()
	if err := zw.Close(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, zipPath)
}
