package api

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// reportRoutes registers the /api/report endpoints (parity with the
// legacy report.py router).
//
// Args:
//
//	r: The chi subrouter.
func (s *Server) reportRoutes(r chi.Router) {
	r.Get("/{reportDate}", s.getReport)
	r.Get("/{reportDate}/video/{cameraID}", s.getReportVideo)
}

// getReport builds the daily report for a date. GET /api/report/{date}
func (s *Server) getReport(w http.ResponseWriter, r *http.Request) {
	dateRaw := chi.URLParam(r, "reportDate")
	day, err := time.ParseInLocation("2006-01-02", dateRaw, time.Local)
	if err != nil {
		writeError(w, http.StatusBadRequest, "report date must be YYYY-MM-DD")
		return
	}
	report, err := s.deps.SnapshotSvc.GetDailyReport(r.Context(), day)
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// getReportVideo generates and streams a plain timelapse MP4 for a
// camera/date (the temp dir is removed after the stream completes).
// GET /api/report/{date}/video/{cameraID}
func (s *Server) getReportVideo(w http.ResponseWriter, r *http.Request) {
	dateRaw := chi.URLParam(r, "reportDate")
	day, err := time.ParseInLocation("2006-01-02", dateRaw, time.Local)
	if err != nil {
		writeError(w, http.StatusBadRequest, "report date must be YYYY-MM-DD")
		return
	}
	cameraID, ok := pathInt64(w, r, "cameraID")
	if !ok {
		return
	}
	videoPath, tempDir, err := s.deps.SnapshotSvc.GenerateDailyVideo(r.Context(), cameraID, day, nil)
	if err != nil {
		log.Warn().Err(err).Msg("daily video generation failed")
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filename := filepath.Base(videoPath)
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	http.ServeFile(w, r, videoPath)
	os.RemoveAll(tempDir)
}

// handleManifest serves the live dashboard manifest consumed by the
// static SPA. GET /api/data/manifest.json
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	manifest, err := s.deps.SnapshotSvc.BuildManifest(r.Context())
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

// handleHealthz reports DB connectivity, storage space and scheduler
// state. GET /api/healthz
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	dbOK := pingDB(r.Context(), s.deps.DB) == nil
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"database": dbOK,
		"time":     time.Now().Format(time.RFC3339),
		"storage":  s.deps.Storage != nil,
		"telegram": s.deps.Notifier.Enabled(),
	})
}
