// Package service contains the application services that orchestrate
// repositories and infrastructure adapters. Each service is constructed
// with injected dependencies (dependency inversion).
//
// StreamService manages continuous MJPEG video streams for cameras. It
// maintains one ffmpeg subprocess per active camera and fans out JPEG frames
// to all subscribed HTTP viewers. The ffmpeg process starts on the first
// Subscribe call and terminates automatically when the last viewer disconnects,
// keeping resource usage minimal when no one is watching.
package service

import (
	"context"
	"fmt"
	"os/exec"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/infrastructure/onvif"
)

// StreamService manages continuous MJPEG streams for cameras. It starts a single
// ffmpeg process per camera and fans out JPEG frames to all subscribed HTTP viewers.
// The process stops automatically when the last viewer disconnects.
//
// Fields:
//
//	onvif: ONVIF client for IP camera URI resolution.
//	fps: Target frames per second for streaming.
//	streams: Active camera streams keyed by camera ID.
//	mu: Read-write lock protecting the streams map and subscriber lists.
type StreamService struct {
	onvif   *onvif.Client
	fps     int
	streams map[int64]*cameraStream
	mu      sync.RWMutex
}

// cameraStream holds the cancel function and subscriber channels for one
// active camera stream. A single ffmpeg goroutine writes frames to all
// subscribers in the slice.
type cameraStream struct {
	cancel      context.CancelFunc
	subscribers []chan []byte
}

// NewStreamService builds a stream service with the given ONVIF client and target FPS.
//
// Args:
//
//	onvif: ONVIF client for IP camera URI resolution.
//	fps: Target frames per second for streaming.
//
// Returns:
//
//	A ready StreamService.
func NewStreamService(onvif *onvif.Client, fps int) *StreamService {
	return &StreamService{
		onvif:   onvif,
		fps:     fps,
		streams: make(map[int64]*cameraStream),
	}
}

// IsStreaming returns true if a camera currently has an active stream.
//
// Args:
//
//	cameraID: Camera identifier.
//
// Returns:
//
//	True if the camera is being streamed.
func (s *StreamService) IsStreaming(cameraID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.streams[cameraID]
	return ok
}

// Subscribe starts a stream for a camera if not running and adds a subscriber.
// If the stream is already active, the caller is attached to the existing fan-out.
//
// Args:
//
//	ctx: Parent context for cancellation.
//	cam: Camera domain entity.
//
// Returns:
//
//	A buffered channel receiving JPEG frames ([]byte), one per frame.
//
// Raises:
//
//	No errors returned today; reserved for future validation.
func (s *StreamService) Subscribe(ctx context.Context, cam domain.Camera) (<-chan []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if stream, ok := s.streams[cam.ID]; ok {
		ch := make(chan []byte, s.fps*2)
		stream.subscribers = append(stream.subscribers, ch)
		return ch, nil
	}

	streamCtx, cancel := context.WithCancel(ctx)
	stream := &cameraStream{
		cancel:      cancel,
		subscribers: make([]chan []byte, 0, 4),
	}
	ch := make(chan []byte, s.fps*2)
	stream.subscribers = append(stream.subscribers, ch)
	s.streams[cam.ID] = stream

	go s.runFFmpeg(streamCtx, cam)
	log.Info().Int64("camera_id", cam.ID).Str("camera_name", cam.Name).Int("fps", s.fps).Msg("stream started")
	return ch, nil
}

// Unsubscribe removes a subscriber and stops the stream if it's the last one.
//
// Args:
//
//	cameraID: Camera identifier.
//	ch: Subscriber channel to remove.
func (s *StreamService) Unsubscribe(cameraID int64, ch <-chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stream, ok := s.streams[cameraID]
	if !ok {
		return
	}

	for i, sub := range stream.subscribers {
		if sub == ch {
			close(sub)
			stream.subscribers = append(stream.subscribers[:i], stream.subscribers[i+1:]...)
			break
		}
	}

	if len(stream.subscribers) == 0 {
		stream.cancel()
		delete(s.streams, cameraID)
		log.Info().Int64("camera_id", cameraID).Msg("stream stopped, no subscribers")
	}
}

// Shutdown stops all active streams immediately.
func (s *StreamService) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, stream := range s.streams {
		stream.cancel()
		for _, sub := range stream.subscribers {
			close(sub)
		}
		delete(s.streams, id)
	}
	log.Info().Msg("all streams shut down")
}

// runFFmpeg launches ffmpeg for a camera and pipes MJPEG frames to subscribers.
// Blocks until the stream ends or the context is cancelled. Cleans up all
// subscriber channels on exit via the deferred function.
//
// Args:
//
//	ctx: Cancellation context (from Subscribe's WithCancel).
//	cam: Camera domain entity with connection details.
func (s *StreamService) runFFmpeg(ctx context.Context, cam domain.Camera) {
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if stream, ok := s.streams[cam.ID]; ok {
			for _, sub := range stream.subscribers {
				close(sub)
			}
			delete(s.streams, cam.ID)
		}
	}()

	var uri string
	var err error
	if cam.CameraType == "usb" {
		if cam.DevicePath == nil || *cam.DevicePath == "" {
			log.Error().Int64("camera_id", cam.ID).Msg("USB stream: device_path missing")
			return
		}
		uri = *cam.DevicePath
	} else {
		uri, err = s.resolveIPStreamURI(cam)
		if err != nil {
			log.Error().Err(err).Int64("camera_id", cam.ID).Msg("IP stream: URI resolution failed")
			return
		}
	}
	log.Info().Int64("camera_id", cam.ID).Str("stream_uri", uri).Msg("stream: resolved URI, starting ffmpeg")

	cmd := s.buildFFmpegCmd(ctx, cam, uri)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Error().Err(err).Int64("camera_id", cam.ID).Msg("stream: stdout pipe failed")
		return
	}
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		log.Error().Err(err).Int64("camera_id", cam.ID).Msg("stream: ffmpeg start failed")
		return
	}
	log.Info().Int64("camera_id", cam.ID).Msg("stream: ffmpeg started")

	// Log ffmpeg stderr in background for debugging.
	go func() {
		buf := make([]byte, 1024)
		for {
			n, rerr := stderr.Read(buf)
			if n > 0 {
				log.Debug().Int64("camera_id", cam.ID).Str("ffmpeg_stderr", string(buf[:n])).Msg("stream: ffmpeg stderr")
			}
			if rerr != nil {
				return
			}
		}
	}()

	buf := make([]byte, 0, 131072)
	tmp := make([]byte, 8192)
	frameCount := 0

	for {
		n, readErr := stdout.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			prev := len(buf)
			buf = s.emitFrames(buf, cam.ID)
			if len(buf) < prev {
				frameCount++
				if frameCount == 1 {
					log.Info().Int64("camera_id", cam.ID).Msg("stream: first frame emitted")
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	_ = cmd.Wait()
	log.Info().Int64("camera_id", cam.ID).Int("frames_emitted", frameCount).Msg("stream: ffmpeg exited")
}

// resolveIPStreamURI tries ONVIF GetBestStreamURI, then falls back to direct RTSP.
// Returns a fully-authenticated RTSP URI (with embedded credentials via Basic Auth
// URL scheme) that ffmpeg can consume directly.
//
// Args:
//
//	cam: Camera domain entity.
//
// Returns:
//
//	The RTSP URI string, or an error when no stream can be resolved.
//
// Raises:
//
//	fmt.Errorf: When no ONVIF profile yields a stream and direct RTSP is unavailable.
func (s *StreamService) resolveIPStreamURI(cam domain.Camera) (string, error) {
	if cam.ProfileToken != nil && *cam.ProfileToken != "" {
		if uri, err := s.onvif.GetStreamURI(cam.Host, cam.Port, cam.Username, cam.Password, *cam.ProfileToken); err == nil {
			return s.onvif.BuildAuthURL(uri, cam.Username, cam.Password), nil
		}
	}
	if uri, _, err := s.onvif.GetBestStreamURI(cam.Host, cam.Port, cam.Username, cam.Password); err == nil && uri != "" {
		return s.onvif.BuildAuthURL(uri, cam.Username, cam.Password), nil
	}
	if cam.Username != "" && cam.Password != "" {
		return fmt.Sprintf("rtsp://%s:%s@%s:554/stream1", cam.Username, cam.Password, cam.Host), nil
	}
	return "", fmt.Errorf("no RTSP stream URI found for camera %s", cam.Name)
}

// buildFFmpegCmd creates an exec.Cmd that outputs a raw MJPEG stream to stdout.
// For USB cameras it reads from a V4L2 device at 640x480; for IP cameras it
// connects to the RTSP URI over TCP with a 5-second read timeout.
//
// Args:
//
//	ctx: Cancellation context (kills ffmpeg on cancel).
//	cam: Camera domain entity (determines IP vs USB command flags).
//	uri: RTSP stream URI or V4L2 device path.
//
// Returns:
//
//	A ready exec.Cmd (call Start() to begin streaming).
func (s *StreamService) buildFFmpegCmd(ctx context.Context, cam domain.Camera, uri string) *exec.Cmd {
	if cam.CameraType == "usb" {
		return exec.CommandContext(ctx, "ffmpeg",
			"-f", "v4l2",
			"-input_format", "mjpeg",
			"-video_size", "640x480",
			"-framerate", fmt.Sprintf("%d", s.fps),
			"-i", uri,
			"-f", "mjpeg",
			"-q:v", "5",
			"-",
		)
	}
	return exec.CommandContext(ctx, "ffmpeg",
		"-rtsp_transport", "tcp",
		"-timeout", "5000000",
		"-i", uri,
		"-f", "mjpeg",
		"-q:v", "5",
		"-framerate", fmt.Sprintf("%d", s.fps),
		"-",
	)
}

// emitFrames scans the buffer for complete JPEG frames and pushes them to subscribers.
// JPEG frames are delimited by the FFD8 (start) and FFD9 (end) byte markers.
// Incomplete trailing data is retained for the next read cycle.
//
// Args:
//
//	buf: Byte buffer containing zero or more JPEG frames.
//	cameraID: Camera identifier for subscriber lookup.
//
// Returns:
//
//	The remaining unconsumed bytes (trailing partial frame).
func (s *StreamService) emitFrames(buf []byte, cameraID int64) []byte {
	for {
		start := findFFD8(buf)
		if start < 0 {
			if len(buf) > 10000 {
				return nil
			}
			return buf
		}

		end := findFFD9(buf[start+2:])
		if end < 0 {
			return buf[start:]
		}

		frame := buf[start : start+2+end+1]

		s.mu.RLock()
		if stream, ok := s.streams[cameraID]; ok {
			for _, sub := range stream.subscribers {
				select {
				case sub <- frame:
				default:
				}
			}
		}
		s.mu.RUnlock()

		buf = buf[start+2+end+1:]
		if len(buf) == 0 {
			return nil
		}
	}
}

// findFFD8 returns the index of the first JPEG SOI marker (0xFFD8) in b, or -1.
func findFFD8(b []byte) int {
	for i := 0; i < len(b)-1; i++ {
		if b[i] == 0xFF && b[i+1] == 0xD8 {
			return i
		}
	}
	return -1
}

// findFFD9 returns the index of the first JPEG EOI marker (0xFFD9) in b, or -1.
func findFFD9(b []byte) int {
	for i := 0; i < len(b)-1; i++ {
		if b[i] == 0xFF && b[i+1] == 0xD9 {
			return i
		}
	}
	return -1
}
