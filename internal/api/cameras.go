package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/domain"
)

// camerasRoutes registers the /api/cameras endpoints (parity with the
// legacy cameras.py router).
//
// Args:
//
//	r: The chi subrouter.
func (s *Server) camerasRoutes(r chi.Router) {
	r.Get("/", s.listCameras)
	r.Post("/", s.createCamera)
	r.Post("/test", s.testCamera)
	r.Route("/{cameraID}", func(r chi.Router) {
		r.Get("/", s.getCamera)
		r.Put("/", s.updateCamera)
		r.Delete("/", s.deleteCamera)
		r.Post("/snapshot", s.forceSnapshot)
		r.Get("/stream", s.streamCamera)
	})
}

// listCameras returns all cameras with dashboard data (latest snapshot
// and total count). GET /api/cameras
func (s *Server) listCameras(w http.ResponseWriter, r *http.Request) {
	data, err := s.deps.SnapshotSvc.GetDashboardData(r.Context())
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// getCamera returns a single camera. GET /api/cameras/{cameraID}
func (s *Server) getCamera(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "cameraID")
	if !ok {
		return
	}
	cam, err := s.deps.CameraSvc.Get(r.Context(), id)
	if err != nil {
		mapRepoErr(w, err, "Camera not found")
		return
	}
	writeJSON(w, http.StatusOK, cam)
}

// createCamera validates and persists a new camera. POST /api/cameras
func (s *Server) createCamera(w http.ResponseWriter, r *http.Request) {
	var payload domain.CameraCreate
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Input validation mirroring the Pydantic constraints.
	if trimSpaces(payload.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if payload.CameraType == "" {
		payload.CameraType = "ip"
	}
	if payload.CameraType != "ip" && payload.CameraType != "usb" {
		writeError(w, http.StatusBadRequest, "camera_type must be 'ip' or 'usb'")
		return
	}
	if payload.CameraType == "usb" {
		if payload.DevicePath == nil || trimSpaces(*payload.DevicePath) == "" {
			writeError(w, http.StatusBadRequest, "device_path is required for USB cameras")
			return
		}
	} else {
		if trimSpaces(payload.Host) == "" {
			writeError(w, http.StatusBadRequest, "host is required for IP cameras")
			return
		}
		if payload.Port < 1 || payload.Port > 65535 {
			writeError(w, http.StatusBadRequest, "port must be between 1 and 65535")
			return
		}
	}
	if payload.IntervalSeconds == 0 {
		payload.IntervalSeconds = s.deps.Cfg.DefaultIntervalSeconds
	}
	if payload.IntervalSeconds < 10 {
		writeError(w, http.StatusBadRequest, "interval_seconds must be >= 10")
		return
	}

	cam, err := s.deps.CameraSvc.Create(r.Context(), payload)
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	if cam.Enabled {
		s.deps.Sched.ScheduleCamera(cam.ID, cam.IntervalSeconds, time.Now())
	}
	log.Info().Int64("id", cam.ID).Str("name", cam.Name).Msg("camera created via API")
	writeJSON(w, http.StatusCreated, cam)
}

// updateCamera applies a partial update and reschedules the capture
// job. PUT /api/cameras/{cameraID}
func (s *Server) updateCamera(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "cameraID")
	if !ok {
		return
	}
	var payload domain.CameraUpdate
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cam, err := s.deps.CameraSvc.Update(r.Context(), id, payload)
	if err != nil {
		mapRepoErr(w, err, "Camera not found")
		return
	}
	if cam.Enabled {
		s.deps.Sched.RescheduleCamera(cam.ID, cam.IntervalSeconds)
	} else {
		s.deps.Sched.RemoveCamera(cam.ID)
	}
	log.Info().Int64("id", id).Msg("camera updated via API")
	writeJSON(w, http.StatusOK, cam)
}

// deleteCamera removes a camera and its capture job. DELETE /api/cameras/{cameraID}
func (s *Server) deleteCamera(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "cameraID")
	if !ok {
		return
	}
	deleted, err := s.deps.CameraSvc.Delete(r.Context(), id)
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "Camera not found")
		return
	}
	s.deps.Sched.RemoveCamera(id)
	log.Info().Int64("id", id).Msg("camera deleted via API")
	w.WriteHeader(http.StatusNoContent)
}

// testCamera probes an ONVIF endpoint without persisting. POST /api/cameras/test
func (s *Server) testCamera(w http.ResponseWriter, r *http.Request) {
	var payload domain.CameraCreate
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Info().Str("host", payload.Host).Int("port", payload.Port).Msg("testing camera via API")
	result, err := s.deps.CameraSvc.Test(r.Context(), payload.Host, payload.Port, payload.Username, payload.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// forceSnapshot triggers an immediate out-of-schedule capture.
// POST /api/cameras/{cameraID}/snapshot
func (s *Server) forceSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "cameraID")
	if !ok {
		return
	}
	cam, err := s.deps.CameraSvc.Get(r.Context(), id)
	if err != nil {
		mapRepoErr(w, err, "Camera not found")
		return
	}
	snap, err := s.deps.SnapshotSvc.Capture(r.Context(), *cam)
	if err != nil {
		mapRepoErr(w, err, "")
		return
	}
	result := domain.SnapshotForceResult{
		CameraID:   snap.CameraID,
		CameraName: cam.Name,
		Success:    snap.Status == "success",
		Error:      snap.ErrorMessage,
	}
	if snap.Status == "success" {
		result.ImagePath = strPtr(snap.ImagePath)
	}
	log.Info().Int64("camera_id", id).Str("status", snap.Status).Msg("force snapshot completed")
	writeJSON(w, http.StatusOK, result)
}
