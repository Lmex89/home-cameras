package api

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// snapshotsRoutes registers the /api/snapshots endpoints (parity with
// the legacy snapshots.py router).
//
// Args:
//
//	r: The chi subrouter.
func (s *Server) snapshotsRoutes(r chi.Router) {
	r.Get("/image/{snapshotID}", s.getSnapshotImage)
	r.Get("/{cameraID}/by-date", s.getCameraSnapshots)
	r.Get("/{snapshotID}", s.getSnapshot)
}

// getSnapshot returns one snapshot with its analysis. GET /api/snapshots/{snapshotID}
func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "snapshotID")
	if !ok {
		return
	}
	snap, err := s.deps.Snaps.GetByID(r.Context(), id)
	if err != nil {
		mapRepoErr(w, err, "Snapshot not found")
		return
	}
	analysesRepo := repository.NewSnapshotAnalysisRepository(s.deps.DB)
	analyses, err := analysesRepo.GetBySnapshot(r.Context(), id)
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	var analysis *domain.SnapshotAnalysis
	if len(analyses) > 0 {
		analysis = &analyses[0]
	}
	writeJSON(w, http.StatusOK, domain.SnapshotWithAnalysis{Snapshot: *snap, Analysis: analysis})
}

// getCameraSnapshots lists a camera's snapshots for a date with their
// analyses. GET /api/snapshots/{cameraID}/by-date?snapshot_date=YYYY-MM-DD
func (s *Server) getCameraSnapshots(w http.ResponseWriter, r *http.Request) {
	cameraID, ok := pathInt64(w, r, "cameraID")
	if !ok {
		return
	}
	dateRaw := r.URL.Query().Get("snapshot_date")
	day, err := time.ParseInLocation("2006-01-02", dateRaw, time.Local)
	if err != nil {
		writeError(w, http.StatusBadRequest, "snapshot_date must be YYYY-MM-DD")
		return
	}
	snaps, err := s.deps.SnapshotSvc.GetCameraSnapshots(r.Context(), cameraID, day)
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	snapshotIDs := make([]int64, 0, len(snaps))
	for _, sn := range snaps {
		snapshotIDs = append(snapshotIDs, sn.ID)
	}
	analysesRepo := repository.NewSnapshotAnalysisRepository(s.deps.DB)
	analyses, err := analysesRepo.GetByCameraAndDate(r.Context(), snapshotIDs)
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	analysisBySnap := map[int64]*domain.SnapshotAnalysis{}
	for i := range analyses {
		analysisBySnap[analyses[i].SnapshotID] = &analyses[i]
	}
	out := make([]domain.SnapshotWithAnalysis, 0, len(snaps))
	for _, sn := range snaps {
		out = append(out, domain.SnapshotWithAnalysis{Snapshot: sn, Analysis: analysisBySnap[sn.ID]})
	}
	writeJSON(w, http.StatusOK, out)
}

// getSnapshotImage streams a snapshot's JPEG, falling back to the ZIP
// archive when the raw file has been rotated away.
// GET /api/snapshots/image/{snapshotID}
func (s *Server) getSnapshotImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "snapshotID")
	if !ok {
		return
	}
	snap, err := s.deps.Snaps.GetByID(r.Context(), id)
	if err != nil {
		mapRepoErr(w, err, "Snapshot not found")
		return
	}
	fullPath := filepath.Join(s.deps.Cfg.SnapshotsDir(), snap.ImagePath)
	if fileExists(fullPath) {
		http.ServeFile(w, r, fullPath)
		return
	}
	if snap.ArchivePath != nil {
		data, err := s.deps.Archive.ReadSnapshot(*snap.ArchivePath)
		if err == nil {
			log.Debug().Int64("snapshot_id", id).Msg("serving snapshot from archive")
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			w.Write(data)
			return
		}
		log.Warn().Err(err).Int64("snapshot_id", id).Msg("archive read failed")
	}
	writeError(w, http.StatusNotFound, "Image file not found")
}
