package api

import (
	"fmt"
	"net/http"

	"github.com/rs/zerolog/log"
)

// streamCamera serves a live MJPEG stream for a camera over HTTP.
// It uses multipart/x-mixed-replace to push JPEG frames to the browser,
// which renders natively in an <img> tag. Subscribes to StreamService
// and unsubscribes on client disconnect (context cancellation).
//
// GET /api/cameras/{cameraID}/stream
func (s *Server) streamCamera(w http.ResponseWriter, r *http.Request) {
	if !s.deps.Cfg.StreamEnabled {
		writeError(w, http.StatusServiceUnavailable, "Live streaming is disabled")
		return
	}

	id, ok := pathInt64(w, r, "cameraID")
	if !ok {
		return
	}
	log.Info().Int64("camera_id", id).Msg("stream: HTTP request received")
	cam, err := s.deps.CameraSvc.Get(r.Context(), id)
	if err != nil {
		log.Error().Err(err).Int64("camera_id", id).Msg("stream: camera lookup failed")
		mapRepoErr(w, err, "Camera not found")
		return
	}
	log.Info().Int64("camera_id", id).Str("camera_name", cam.Name).Str("camera_type", cam.CameraType).Msg("stream: camera found, subscribing")

	ch, err := s.deps.StreamSvc.Subscribe(r.Context(), *cam)
	if err != nil {
		log.Error().Err(err).Int64("camera_id", id).Msg("stream subscription failed")
		writeError(w, http.StatusInternalServerError, "Failed to start stream")
		return
	}
	defer s.deps.StreamSvc.Unsubscribe(id, ch)

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	frameCount := 0
	for {
		select {
		case <-r.Context().Done():
			log.Info().Int64("camera_id", id).Int("frames_sent", frameCount).Msg("stream client disconnected")
			return
		case frame, ok := <-ch:
			if !ok {
				log.Warn().Int64("camera_id", id).Int("frames_sent", frameCount).Msg("stream channel closed")
				return
			}
			if _, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame)); err != nil {
				log.Error().Err(err).Int64("camera_id", id).Msg("stream: write header failed")
				return
			}
			if _, err := w.Write(frame); err != nil {
				log.Error().Err(err).Int64("camera_id", id).Msg("stream: write frame failed")
				return
			}
			if _, err := w.Write([]byte("\r\n")); err != nil {
				return
			}
			frameCount++
			if frameCount%30 == 0 {
				log.Debug().Int64("camera_id", id).Int("frames_sent", frameCount).Int("frame_bytes", len(frame)).Msg("stream: heart beat")
			}
			flusher.Flush()
		}
	}
}
