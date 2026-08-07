// Package web embeds the frontend assets (the standalone dashboard SPA,
// the reviews page, and the static CSS/JS) into the binary so the
// server needs no external files at runtime. The files are byte-copies
// of the project-root index.html/reviews.html and app/web/static/*.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var staticFS embed.FS

// ServeHTML writes an embedded HTML page.
//
// Args:
//
//	w: The response writer.
//	r: The incoming request.
//	name: The embedded file name (e.g. "index.html").
func ServeHTML(w http.ResponseWriter, r *http.Request, name string) {
	data, err := staticFS.ReadFile("static/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// ServeStatic serves an embedded file from the static/ directory,
// guarding against path traversal. GET /static/*
//
// Args:
//
//	w: The response writer.
//	r: The incoming request.
func ServeStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/static/")
	if strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.StripPrefix("/static/", http.FileServer(http.FS(sub))).ServeHTTP(w, r)
}
