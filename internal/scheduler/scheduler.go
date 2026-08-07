// Package scheduler runs the periodic jobs of the camera monitor: a
// per-camera interval scheduler (snapshot capture with restart-anchored
// intervals, mirroring APScheduler IntervalTrigger) and a cron-driven
// daily jobs (retention 06:00, timelapse 21:00) plus the analysis and
// health check tickers.
//
// Every job runs through a guarded wrapper: panics are recovered, logged
// as errors, and reported to Telegram via the alarm channel (parity with
// the legacy APScheduler event listener).
package scheduler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/config"
)

// Scheduler owns all periodic jobs of the application.
type Scheduler struct {
	cfg config.Config

	cameraCapture func(ctx context.Context, cameraID int64)
	analysisTick  func(ctx context.Context)
	retentionRun  func(ctx context.Context)
	timelapseRun  func(ctx context.Context, day time.Time)
	healthRun     func(ctx context.Context)

	mu     sync.Mutex
	timers map[int64]*time.Timer
	paused bool
	cron   *cron.Cron
}

// Options wires the scheduler to the application services (injected by
// the wiring layer in cmd/server/main.go).
type Options struct {
	CameraCapture func(ctx context.Context, cameraID int64)
	AnalysisTick  func(ctx context.Context)
	RetentionRun  func(ctx context.Context)
	TimelapseRun  func(ctx context.Context, day time.Time)
	HealthRun     func(ctx context.Context)
}

// New builds a scheduler around the given job callbacks.
//
// Args:
//
//	cfg: Application configuration (cron times, intervals).
//	opts: Job callbacks provided by the wiring layer.
//
// Returns:
//
//	A stopped Scheduler (call Start).
func New(cfg config.Config, opts Options) *Scheduler {
	s := &Scheduler{
		cfg:           cfg,
		cameraCapture: opts.CameraCapture,
		analysisTick:  opts.AnalysisTick,
		retentionRun:  opts.RetentionRun,
		timelapseRun:  opts.TimelapseRun,
		healthRun:     opts.HealthRun,
		timers:        map[int64]*time.Timer{},
		cron:          cron.New(cron.WithLocation(localLocation(cfg.Timezone))),
	}
	s.registerDailyJobs()
	return s
}

// localLocation loads the configured timezone, defaulting to UTC on
// load failure (the cron library needs a concrete *time.Location).
//
// Args:
//
//	tz: IANA timezone name (e.g. "America/Mexico_City").
//
// Returns:
//
//	The parsed location (UTC fallback).
func localLocation(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Warn().Err(err).Str("tz", tz).Msg("invalid timezone, falling back to UTC")
		return time.UTC
	}
	return loc
}

// registerDailyJobs adds the cron-driven jobs: daily retention at 06:00,
// daily timelapse (config hour/minute), the analysis ticker and the
// health check ticker.
func (s *Scheduler) registerDailyJobs() {
	s.cron.AddFunc("0 6 * * *", s.guard(func(ctx context.Context) {
		log.Info().Msg("retention cron fired")
		s.retentionRun(ctx)
	}))
	if s.cfg.TimelapseEnabled {
		s.cron.AddFunc(
			fmt.Sprintf("%d %d * * *", s.cfg.TimelapseMinute, s.cfg.TimelapseHour),
			s.guard(func(ctx context.Context) {
				yesterday := time.Now().AddDate(0, 0, -1)
				log.Info().Time("day", yesterday).Msg("timelapse cron fired")
				s.timelapseRun(ctx, yesterday)
			}),
		)
	}
	s.cron.AddFunc(fmt.Sprintf("@every %ds", s.cfg.AnalysisIntervalSeconds), s.guard(func(ctx context.Context) {
		s.analysisTick(ctx)
	}))
	s.cron.AddFunc(fmt.Sprintf("@every %dm", s.cfg.HealthCheckIntervalMin), s.guard(func(ctx context.Context) {
		s.healthRun(ctx)
	}))
}

// Start launches the cron scheduler.
func (s *Scheduler) Start() {
	s.cron.Start()
	log.Info().Msg("scheduler started")
}

// Stop shuts down the cron scheduler and cancels all camera timers.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := s.cron.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
	}
	for id, t := range s.timers {
		t.Stop()
		delete(s.timers, id)
	}
	log.Info().Msg("scheduler stopped")
}

// ScheduleCamera starts (or replaces) the capture timer of a camera.
// The first fire is anchored to start so the schedule survives server
// restarts (parity with the legacy load_schedule start_date logic).
//
// Args:
//
//	cameraID: Camera primary key.
//	interval: Seconds between captures.
//	start: Anchor time for the first fire.
func (s *Scheduler) ScheduleCamera(cameraID int64, interval int, start time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.timers[cameraID]; ok {
		old.Stop()
	}
	delay := time.Until(start)
	if delay < 0 {
		delay = 0
	}
	if delay > time.Duration(interval)*time.Second {
		delay = time.Duration(interval) * time.Second
	}
	timer := time.AfterFunc(delay, func() {
		s.fireCamera(cameraID, interval)
	})
	s.timers[cameraID] = timer
	log.Info().Int64("camera_id", cameraID).Int("interval_s", interval).Msg("camera capture scheduled")
}

// RemoveCamera stops and forgets a camera's capture timer.
//
// Args:
//
//	cameraID: Camera primary key.
func (s *Scheduler) RemoveCamera(cameraID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.timers[cameraID]; ok {
		t.Stop()
		delete(s.timers, cameraID)
		log.Info().Int64("camera_id", cameraID).Msg("camera capture job removed")
	}
}

// RescheduleCamera replaces a camera's interval (used after updates).
//
// Args:
//
//	cameraID: Camera primary key.
//	interval: New interval in seconds.
func (s *Scheduler) RescheduleCamera(cameraID int64, interval int) {
	s.ScheduleCamera(cameraID, interval, time.Now())
}

// PauseAll prevents camera captures from starting new work; pending
// timer fires reschedule themselves until ResumeAll is called. Used by
// retention to avoid SQLite write contention.
func (s *Scheduler) PauseAll() {
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	log.Info().Msg("camera capture jobs paused")
}

// ResumeAll re-enables camera captures.
func (s *Scheduler) ResumeAll() {
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
	log.Info().Msg("camera capture jobs resumed")
}

// fireCamera runs one capture for a camera and reschedules the next
// fire after the interval. When paused, retries shortly.
//
// Args:
//
//	cameraID: Camera primary key.
//	interval: Seconds between captures.
func (s *Scheduler) fireCamera(cameraID int64, interval int) {
	s.mu.Lock()
	paused := s.paused
	s.mu.Unlock()
	if paused {
		time.AfterFunc(5*time.Second, func() { s.fireCamera(cameraID, interval) })
		return
	}
	s.guard(func(ctx context.Context) { s.cameraCapture(ctx, cameraID) })()
	s.mu.Lock()
	if t, ok := s.timers[cameraID]; ok {
		t.Reset(time.Duration(interval) * time.Second)
	}
	s.mu.Unlock()
}

// guard wraps a job with panic recovery: panics are logged and reported
// to Telegram via the alarm channel (parity with the legacy
// _alarm_listener). Returns a no-arg func compatible with cron.AddFunc.
//
// Args:
//
//	job: The job callback.
//
// Returns:
//
//	A safe wrapper callable by the cron scheduler.
func (s *Scheduler) guard(job func(ctx context.Context)) func() {
	return func() {
		ctx := context.Background()
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("scheduled job panicked")
				SendAlarm(s.cfg, fmt.Sprintf("Camera Monitor ALARM\nJob panicked: %v", r))
			}
		}()
		job(ctx)
	}
}

// SendAlarm posts an alarm message to Telegram using a direct HTTP call
// (parity with the legacy _send_alarm_sync). Never blocks the job.
//
// Args:
//
//	cfg: Application config (telegram credentials).
//	msg: The alarm text.
func SendAlarm(cfg config.Config, msg string) {
	if !cfg.TelegramEnabled || cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" {
		log.Debug().Msg("telegram disabled; would send alarm")
		return
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.TelegramBotToken)
	body := fmt.Sprintf(`{"chat_id":%q,"text":%q}`, cfg.TelegramChatID, msg)
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		log.Error().Err(err).Msg("failed to send alarm notification")
		return
	}
	resp.Body.Close()
	log.Info().Int("status", resp.StatusCode).Msg("alarm notification sent")
}
