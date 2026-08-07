package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/domain"
)

// videosRoutes registers the /api/videos endpoints (parity with the
// legacy videos.py router).
//
// Args:
//
//	r: The chi subrouter.
func (s *Server) videosRoutes(r chi.Router) {
	r.Post("/", s.createVideo)
	r.Post("/annotated", s.createAnnotatedVideo)
	r.Get("/download/{filename}", s.downloadVideo)
}

// createVideo generates a plain timelapse MP4 and persists it under
// data/videos. POST /api/videos
func (s *Server) createVideo(w http.ResponseWriter, r *http.Request) {
	var payload domain.VideoRequest
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cctx, cancel := requestTimeout(r, 30*time.Minute)
	defer cancel()

	outputPath, tempDir, err := s.deps.SnapshotSvc.GenerateDailyVideo(cctx, payload.CameraID, payload.Date.Time(), payload.Hour)
	if err != nil {
		log.Warn().Err(err).Msg("video generation rejected")
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	suffix := ""
	if payload.Hour != nil {
		suffix = "_h" + timeString(payload.Hour)
	}
	vdir := s.deps.Cfg.VideosDir()
	os.MkdirAll(vdir, 0o755)
	persistent := filepath.Join(vdir, "timelapse_"+itosa(payload.CameraID)+"_"+payload.Date.Time().Format("2006-01-02")+suffix+".mp4")
	if err := moveFile(outputPath, persistent); err != nil {
		os.RemoveAll(tempDir)
		mapRepoErr(w, err, "")
		return
	}
	os.RemoveAll(tempDir)
	url := "/api/videos/download/" + filepath.Base(persistent)
	log.Info().Str("url", url).Msg("video saved")
	writeJSON(w, http.StatusOK, domain.VideoResponse{VideoURL: url})
}

// createAnnotatedVideo generates an annotated timelapse with detection
// overlays, uploads it to storage when configured, and schedules the
// Telegram notification in a background goroutine.
// POST /api/videos/annotated
func (s *Server) createAnnotatedVideo(w http.ResponseWriter, r *http.Request) {
	var payload domain.AnnotatedVideoRequest
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var classes map[string]bool
	if payload.Classes != nil && strings.TrimSpace(*payload.Classes) != "" {
		classes = map[string]bool{}
		for _, c := range strings.Split(*payload.Classes, ",") {
			if c = strings.TrimSpace(c); c != "" {
				classes[c] = true
			}
		}
	}
	cctx, cancel := requestTimeout(r, 30*time.Minute)
	defer cancel()

	cam, err := s.deps.CameraSvc.Get(cctx, payload.CameraID)
	if err != nil {
		mapRepoErr(w, err, "Camera not found")
		return
	}

	outputPath, tempDir, err := s.deps.Timelapse.RenderAnnotated(cctx, payload.CameraID, payload.Date.Time(), classes)
	if err != nil {
		log.Warn().Err(err).Msg("annotated video rejected")
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	vdir := s.deps.Cfg.VideosDir()
	os.MkdirAll(vdir, 0o755)
	persistent := filepath.Join(vdir, "timelapse_annotated_"+itosa(payload.CameraID)+"_"+payload.Date.Time().Format("2006-01-02")+".mp4")
	if err := moveFile(outputPath, persistent); err != nil {
		os.RemoveAll(tempDir)
		mapRepoErr(w, err, "")
		return
	}
	os.RemoveAll(tempDir)
	log.Info().Str("path", persistent).Msg("annotated video saved")

	// Upload to storage (best effort) and notify Telegram in background.
	var blazeURL string
	if s.deps.Storage != nil {
		if url, err := s.deps.Storage.Upload(r.Context(), persistent); err == nil {
			blazeURL = url
			log.Info().Str("url", url).Msg("annotated video uploaded to storage")
		} else {
			log.Warn().Err(err).Str("path", persistent).Msg("annotated video upload to storage failed")
		}
	}

	// The request context is cancelled when this handler returns, so the
	// background notification gets its own bounded context.
	go func() {
		notifyCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		sizeMB := fileSizeMB(persistent)
		caption := "\U0001f3a5 Camera " + cam.Name + " — timelapse " + payload.Date.Time().Format("2006-01-02") + " (annotated, " + sizeMB + " MB)"
		if !s.deps.Notifier.SendVideo(notifyCtx, persistent, caption,
			"http://localhost:"+itoa(s.deps.Cfg.Port)+"/api/videos/download/"+filepath.Base(persistent),
			blazeURL) {
			log.Warn().Str("path", persistent).Msg("telegram notification for annotated video failed")
		}
	}()

	url := "/api/videos/download/" + filepath.Base(persistent)
	if blazeURL != "" {
		url = blazeURL
	}
	writeJSON(w, http.StatusOK, domain.AnnotatedVideoResponse{VideoURL: url})
}

// downloadVideo serves a previously generated MP4, falling back to the
// archive. GET /api/videos/download/{filename}
func (s *Server) downloadVideo(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if strings.ContainsAny(filename, "/\\") {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	full := filepath.Join(s.deps.Cfg.VideosDir(), filename)
	if fileExists(full) {
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeFile(w, r, full)
		return
	}
	data, err := s.deps.Archive.ReadVideo(filename)
	if err == nil {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
		return
	}
	log.Warn().Str("filename", filename).Msg("video download: file not found")
	writeError(w, http.StatusNotFound, "Video not found")
}
