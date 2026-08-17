package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/domain"
)

// reviewsRoutes registers the /api/reviews endpoints (parity with the
// legacy reviews.py router).
//
// Args:
//
//	r: The chi subrouter.
func (s *Server) reviewsRoutes(r chi.Router) {
	r.Get("/pending", s.listPendingReviews)
	r.Get("/count", s.countPendingReviews)
	r.Get("/detections", s.listDetections)
	r.Post("/bulk-review", s.bulkReview)
	r.Post("/{analysisID}/review", s.updateReview)
}

// listPendingReviews returns snapshots flagged for human review.
// GET /api/reviews/pending
func (s *Server) listPendingReviews(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.AnalysisSvc.GetPendingReviews(r.Context(), 5000)
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// countPendingReviews returns the review queue size. GET /api/reviews/count
func (s *Server) countPendingReviews(w http.ResponseWriter, r *http.Request) {
	count, err := s.deps.AnalysisSvc.CountPendingReviews(r.Context())
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

// listDetections returns the paginated detection browser.
// GET /api/reviews/detections
func (s *Server) listDetections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	daysBack := intQuery(q.Get("days_back"), 1)
	if daysBack < 0 || daysBack > 30 {
		writeError(w, http.StatusBadRequest, "days_back must be between 0 and 30")
		return
	}
	limit := intQuery(q.Get("limit"), 500)
	if limit < 1 || limit > 2000 {
		writeError(w, http.StatusBadRequest, "limit must be between 1 and 2000")
		return
	}
	offset := intQuery(q.Get("offset"), 0)
	cameraID := int64Query(q.Get("camera_id"), 0)
	className := q.Get("class_name")
	dateFrom := q.Get("date_from")

	rows, err := s.deps.AnalysisSvc.GetDetections(r.Context(), daysBack, cameraID, className, limit, offset, dateFrom)
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// bulkReview updates several analyses at once. POST /api/reviews/bulk-review
func (s *Server) bulkReview(w http.ResponseWriter, r *http.Request) {
	var payload domain.BulkReviewPayload
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(payload.AnalysisIDs) == 0 {
		writeError(w, http.StatusBadRequest, "analysis_ids must not be empty")
		return
	}
	updated := 0
	errorsOut := []map[string]any{}
	for _, id := range payload.AnalysisIDs {
		analysis, err := s.deps.AnalysisSvc.UpdateReview(r.Context(), id, payload.ReviewRequired, payload.ReviewReason)
		if err != nil {
			errorsOut = append(errorsOut, map[string]any{"id": id, "error": err.Error()})
			continue
		}
		if analysis == nil {
			errorsOut = append(errorsOut, map[string]any{"id": id, "error": "Not found"})
			continue
		}
		updated++
	}
	log.Info().Int("updated", updated).Int("errors", len(errorsOut)).Msg("bulk review complete")
	writeJSON(w, http.StatusOK, map[string]any{"updated": updated, "errors": errorsOut})
}

// updateReview changes the review decision of one analysis.
// POST /api/reviews/{analysisID}/review
func (s *Server) updateReview(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "analysisID")
	if !ok {
		return
	}
	var payload domain.AnalysisReviewUpdate
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	analysis, err := s.deps.AnalysisSvc.UpdateReview(r.Context(), id, payload.ReviewRequired, payload.ReviewReason)
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	if analysis == nil {
		writeError(w, http.StatusNotFound, "Analysis not found")
		return
	}
	log.Info().Int64("analysis_id", id).Msg("review updated via API")
	writeJSON(w, http.StatusOK, analysis)
}

// intQuery parses a query value as int with a fallback.
//
// Args:
//
//	raw: The raw query value.
//	fallback: Value used when raw is empty or invalid.
//
// Returns:
//
//	The parsed integer.
func intQuery(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := parseInt(raw)
	if err != nil {
		return fallback
	}
	return n
}

// int64Query parses a query value as int64 with a fallback.
//
// Args:
//
//	raw: The raw query value.
//	fallback: Value used when raw is empty or invalid.
//
// Returns:
//
//	The parsed integer.
func int64Query(raw string, fallback int64) int64 {
	if raw == "" {
		return fallback
	}
	n, err := parseInt64(raw)
	if err != nil {
		return fallback
	}
	return n
}

// parseInt parses a base-10 integer.
//
// Args:
//
//	raw: The string to parse.
//
// Returns:
//
//	The parsed value and any error.
func parseInt(raw string) (int, error) {
	var n int
	_, err := fmt.Sscan(raw, &n)
	return n, err
}

// parseInt64 parses a base-10 64-bit integer.
//
// Args:
//
//	raw: The string to parse.
//
// Returns:
//
//	The parsed value and any error.
func parseInt64(raw string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(raw, &n)
	return n, err
}
