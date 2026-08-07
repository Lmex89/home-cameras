package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/domain"
)

// retentionRoutes registers the /api/retention endpoints (parity with
// the legacy trigger_retention / trigger_purge endpoints in main.py).
// Both pause capture/analysis jobs while running to avoid SQLite write
// contention, resuming them afterwards.
//
// Args:
//
//	r: The chi subrouter.
func (s *Server) retentionRoutes(r chi.Router) {
	r.Post("/run", s.runRetention)
	r.Post("/purge", s.purgeRetention)
}

// runRetention triggers the full retention pipeline (zip + delete).
// POST /api/retention/run
func (s *Server) runRetention(w http.ResponseWriter, r *http.Request) {
	log.Info().Msg("manual retention: pausing capture/analysis schedulers")
	s.deps.Sched.PauseAll()
	defer s.deps.Sched.ResumeAll()

	result, err := s.deps.Retention.Run(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("manual retention run failed")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// purgeRetention permanently deletes data older than the given days.
// POST /api/retention/purge
func (s *Server) purgeRetention(w http.ResponseWriter, r *http.Request) {
	var payload domain.PurgeRequest
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if payload.Days < 1 {
		writeError(w, http.StatusBadRequest, "days must be >= 1")
		return
	}
	log.Warn().Int("days", payload.Days).Msg("manual purge: pausing capture/analysis schedulers")
	s.deps.Sched.PauseAll()
	defer s.deps.Sched.ResumeAll()

	result, err := s.deps.Retention.PurgeOlderThan(r.Context(), payload.Days)
	if err != nil {
		log.Error().Err(err).Msg("manual purge failed")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
