package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestServeHTML verifies embedded pages render or 404 cleanly.
func TestServeHTML(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	w := httptest.NewRecorder()
	ServeHTML(w, req, "index.html")
	if w.Code != http.StatusOK {
		t.Fatalf("index: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/missing.html", nil)
	w = httptest.NewRecorder()
	ServeHTML(w, req, "missing.html")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", w.Code)
	}
}

// TestServeStatic verifies static assets and the traversal guard.
func TestServeStatic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil)
	w := httptest.NewRecorder()
	ServeStatic(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("app.js: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/static/../secret", nil)
	w = httptest.NewRecorder()
	ServeStatic(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("traversal: %d", w.Code)
	}
}
