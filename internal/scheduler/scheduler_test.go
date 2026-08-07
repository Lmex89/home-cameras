package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lmex89/home-cameras/internal/config"
)

// noopOptions returns scheduler options with harmless callbacks.
func noopOptions() Options {
	return Options{
		CameraCapture: func(ctx context.Context, cameraID int64) {},
		AnalysisTick:  func(ctx context.Context) {},
		RetentionRun:  func(ctx context.Context) {},
		TimelapseRun:  func(ctx context.Context, day time.Time) {},
		HealthRun:     func(ctx context.Context) {},
	}
}

// TestLocalLocation verifies timezone resolution and UTC fallback.
func TestLocalLocation(t *testing.T) {
	loc := localLocation("America/Mexico_City")
	if loc == nil || loc.String() != "America/Mexico_City" {
		t.Fatalf("loc = %v", loc)
	}
	if got := localLocation("Not/AZone"); got != time.UTC {
		t.Fatalf("fallback = %v want UTC", got)
	}
}

// TestGuardRecoversPanic verifies job panics are recovered without
// crashing the process and that normal jobs still run.
func TestGuardRecoversPanic(t *testing.T) {
	cfg := config.Config{TelegramEnabled: false}
	s := New(cfg, noopOptions())

	wrapped := s.guard(func(ctx context.Context) {
		panic("boom")
	})
	wrapped() // reaching here means the panic was recovered

	ran := false
	wrapped = s.guard(func(ctx context.Context) {
		ran = true
	})
	wrapped()
	if !ran {
		t.Fatal("job body did not run")
	}
}

// TestScheduleCameraAndFire verifies a camera timer fires repeatedly at
// its interval and Stop halts it.
func TestScheduleCameraAndFire(t *testing.T) {
	cfg := config.Config{}
	var fires atomic.Int32
	opts := noopOptions()
	opts.CameraCapture = func(ctx context.Context, cameraID int64) {
		fires.Add(1)
	}
	s := New(cfg, opts)
	defer s.Stop()

	s.ScheduleCamera(1, 1, time.Now())

	waitFor(t, func() bool { return fires.Load() >= 2 }, 5*time.Second, "camera should fire twice")
	s.Stop()
	time.Sleep(300 * time.Millisecond)
	after := fires.Load()
	time.Sleep(1200 * time.Millisecond)
	if fires.Load() != after {
		t.Fatalf("captures kept firing after Stop: %d -> %d", after, fires.Load())
	}
}

// TestPauseResume verifies paused timers defer capture until resume.
func TestPauseResume(t *testing.T) {
	cfg := config.Config{}
	var fires atomic.Int32
	opts := noopOptions()
	opts.CameraCapture = func(ctx context.Context, cameraID int64) {
		fires.Add(1)
	}
	s := New(cfg, opts)
	defer s.Stop()

	s.PauseAll()
	s.fireCamera(1, 60) // schedules a retry in 5s
	time.Sleep(200 * time.Millisecond)
	if fires.Load() != 0 {
		t.Fatal("capture must not run while paused")
	}
	s.ResumeAll()
	waitFor(t, func() bool { return fires.Load() >= 1 }, 8*time.Second, "retry capture after resume")
}

// TestRemoveAndRescheduleCamera verifies job lifecycle bookkeeping.
func TestRemoveAndRescheduleCamera(t *testing.T) {
	cfg := config.Config{}
	var fires atomic.Int32
	opts := noopOptions()
	opts.CameraCapture = func(ctx context.Context, cameraID int64) {
		fires.Add(1)
	}
	s := New(cfg, opts)
	defer s.Stop()

	s.ScheduleCamera(7, 1, time.Now())
	waitFor(t, func() bool { return fires.Load() >= 1 }, 3*time.Second, "first fire")

	s.RemoveCamera(7)
	time.Sleep(300 * time.Millisecond)
	after := fires.Load()
	time.Sleep(1200 * time.Millisecond)
	if fires.Load() != after {
		t.Fatalf("captures continued after RemoveCamera: %d -> %d", after, fires.Load())
	}

	s.ScheduleCamera(7, 1, time.Now())
	waitFor(t, func() bool { return fires.Load() > after }, 3*time.Second, "fire after reschedule")
}

// TestStartStop verifies the cron lifecycle is safe to start and stop.
func TestStartStop(t *testing.T) {
	cfg := config.Config{TimelapseEnabled: true, TimelapseHour: 21, TimelapseMinute: 0}
	s := New(cfg, noopOptions())
	s.Start()
	s.Stop()
	s.Stop() // idempotent
}

// waitFor polls cond until it returns true or the timeout expires.
func waitFor(t *testing.T, cond func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}
