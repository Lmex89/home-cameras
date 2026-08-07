package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Lmex89/home-cameras/internal/web"
)

// handleIndex serves the embedded standalone dashboard SPA. GET /index.html
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	web.ServeHTML(w, r, "index.html")
}

// handleReviews serves the embedded standalone review dashboard page.
// GET /reviews.html
func (s *Server) handleReviews(w http.ResponseWriter, r *http.Request) {
	web.ServeHTML(w, r, "reviews.html")
}

// handleSnapshotFiles serves raw snapshot files from the snapshots
// directory (legacy static mount /snapshots). Path traversal is
// prevented by cleaning and prefix-checking the resolved path.
// GET /snapshots/*
func (s *Server) handleSnapshotFiles(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/snapshots/")
	clean := filepath.Clean("/" + rel)
	if strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(s.deps.Cfg.SnapshotsDir(), rel)
	if !fileExists(full) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}
