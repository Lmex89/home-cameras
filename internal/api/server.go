// Package api wires the HTTP server: a chi router with CORS/logging
// middleware, the API handlers, the embedded web pages, and static
// asset serving. Handlers are thin: they validate input, call services,
// and map errors to HTTP status codes.
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/infrastructure/archive"
	"github.com/Lmex89/home-cameras/internal/infrastructure/onvif"
	"github.com/Lmex89/home-cameras/internal/infrastructure/storage"
	"github.com/Lmex89/home-cameras/internal/infrastructure/telegram"
	"github.com/Lmex89/home-cameras/internal/repository"
	"github.com/Lmex89/home-cameras/internal/scheduler"
	"github.com/Lmex89/home-cameras/internal/service"
	"github.com/Lmex89/home-cameras/internal/web"
)

// Deps bundles every dependency handlers need. Built once in
// cmd/server/main.go and injected via the Server struct (dependency
// injection, mirroring the legacy api/deps.py).
type Deps struct {
	Cfg      config.Config
	DB       *sqlx.DB
	Cameras  *repository.CameraRepository
	Snaps    *repository.SnapshotRepository
	Onvif    *onvif.Client
	Archive  *archive.Reader
	Notifier *telegram.Notifier
	Storage  *storage.S3
	Sched    *scheduler.Scheduler

	CameraSvc   *service.CameraService
	SnapshotSvc *service.SnapshotService
	AnalysisSvc *service.AnalysisService
	Retention   *service.RetentionService
	Timelapse   *service.TimelapseService
}

// Server owns the HTTP routes and the injected dependencies.
type Server struct {
	deps Deps
}

// NewServer builds a Server with the given dependencies.
//
// Args:
//
//	deps: All application dependencies.
//
// Returns:
//
//	A ready Server (call Router to get the http.Handler).
func NewServer(deps Deps) *Server {
	return &Server{deps: deps}
}

// Router assembles the full http.Handler for the application.
//
// Returns:
//
//	The chi router with all routes registered.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// Middleware stack: request id, recovery, request logging, CORS
	// (parity with the legacy CORSMiddleware allow-all config).
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(hlog.RequestIDHandler("request_id", "Request-Id"))
	r.Use(s.requestLogger)
	r.Use(corsMiddleware)

	// Web pages and static assets.
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/index.html", http.StatusTemporaryRedirect)
	})
	r.Get("/index.html", s.handleIndex)
	r.Get("/reviews.html", s.handleReviews)
	r.Get("/static/*", func(w http.ResponseWriter, r *http.Request) {
		web.ServeStatic(w, r)
	})
	r.Get("/snapshots/*", s.handleSnapshotFiles)

	// API routers.
	r.Route("/api", func(r chi.Router) {
		r.Get("/healthz", s.handleHealthz)
		r.Get("/data/manifest.json", s.handleManifest)

		r.Route("/cameras", s.camerasRoutes)
		r.Route("/snapshots", s.snapshotsRoutes)
		r.Route("/report", s.reportRoutes)
		r.Route("/videos", s.videosRoutes)
		r.Route("/reviews", s.reviewsRoutes)
		r.Route("/retention", s.retentionRoutes)
	})
	return r
}

// requestLogger logs every request with status and latency (zerolog
// wrapper around chi's middleware).
//
// Args:
//
//	next: The next handler.
//
// Returns:
//
//	A logging http.Handler.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		latency := time.Since(start)
		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", ww.Status()).
			Dur("latency", latency).
			Msg("request")
	})
}

// corsMiddleware allows all origins/methods/headers (parity with the
// legacy CORSMiddleware allow_origins=["*"]).
//
// Args:
//
//	next: The next handler.
//
// Returns:
//
//	An http.Handler with CORS headers set.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Shutdown flushes nothing extra today; kept for future graceful
// drain hooks.
func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}
