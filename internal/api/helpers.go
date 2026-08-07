package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/database"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// fileExists reports whether a file exists on disk.
//
// Args:
//
//	path: The filesystem path to check.
//
// Returns:
//
//	True when the path names an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// pingDB verifies database connectivity with a short timeout.
//
// Args:
//
//	ctx: Request context.
//	db: The database pool.
//
// Returns:
//
//	Any ping error (nil when healthy).
func pingDB(ctx context.Context, db *sqlx.DB) error {
	return database.PingWithTimeout(ctx, db)
}

// requestTimeout returns a context with a bounded deadline for
// long-running endpoint work (video generation, captures).
//
// Args:
//
//	r: The incoming request.
//	d: The maximum duration.
//
// Returns:
//
//	The bounded context and its cancel function.
func requestTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// itosa formats an int64 for filename construction.
//
// Args:
//
//	n: The value to format.
//
// Returns:
//
//	The decimal string.
func itosa(n int64) string { return strconv.FormatInt(n, 10) }

// itoa formats an int for message construction.
//
// Args:
//
//	n: The value to format.
//
// Returns:
//
//	The decimal string.
func itoa(n int) string { return strconv.Itoa(n) }

// timeString formats an hour pointer for filename suffixes.
//
// Args:
//
//	h: The hour pointer (nil renders empty).
//
// Returns:
//
//	Zero-padded two-digit hour.
func timeString(h *int) string {
	if h == nil {
		return ""
	}
	return strconv.FormatInt(int64(*h), 10)
}

// moveFile renames src to dst (falling back to copy when the rename
// crosses devices).
//
// Args:
//
//	src: Source path.
//	dst: Destination path.
//
// Returns:
//
//	Any I/O error.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// fileSizeMB formats a file size as megabytes (one decimal).
//
// Args:
//
//	path: The file to measure.
//
// Returns:
//
//	The size string like "12.3".
func fileSizeMB(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "0.0"
	}
	return strconv.FormatFloat(float64(info.Size())/(1024*1024), 'f', 1, 64)
}

// writeJSON serializes v as JSON with the given status code, mirroring
// the API contract of the original implementation.
//
// Args:
//
//	w: The response writer.
//	status: HTTP status code.
//	v: The value to serialize.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error().Err(err).Msg("failed to encode JSON response")
	}
}

// writeError writes a JSON error body {"detail": msg} with the status.
//
// Args:
//
//	w: The response writer.
//	status: HTTP status code.
//	msg: The error detail.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"detail": msg})
}

// readJSON decodes a request body into v, reporting malformed JSON as
// a 400. Unknown fields are rejected.
//
// Args:
//
//	r: The incoming request.
//	v: The destination struct.
//
// Returns:
//
//	An error when the body is invalid.
func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	return nil
}

// pathInt64 parses a chi URL parameter as int64, writing a 400 on
// failure.
//
// Args:
//
//	w: The response writer.
//	r: The incoming request.
//	name: The URL parameter name.
//
// Returns:
//
//	The parsed value and whether parsing succeeded.
func pathInt64(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	raw := chi.URLParam(r, name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name+" parameter")
		return 0, false
	}
	return id, true
}

// mapRepoErr converts repository errors to HTTP status codes: ErrNotFound
// becomes 404, anything else 500.
//
// Args:
//
//	w: The response writer.
//	err: The repository error.
//	notFoundMsg: Detail used for 404 responses.
//
// Returns:
//
//	True when the error was handled (caller should stop).
func mapRepoErr(w http.ResponseWriter, err error, notFoundMsg string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, notFoundMsg)
		return true
	}
	log.Error().Err(err).Msg("internal error")
	writeError(w, http.StatusInternalServerError, "Internal server error")
	return true
}

// strPtr returns a pointer to s, or nil for empty strings (used when
// mapping optional API fields).
//
// Args:
//
//	s: The string value.
//
// Returns:
//
//	A pointer when s is non-empty, nil otherwise.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// trimSpaces strips surrounding whitespace from a string.
//
// Args:
//
//	s: The string to trim.
//
// Returns:
//
//	The trimmed string.
func trimSpaces(s string) string {
	return strings.TrimSpace(s)
}
