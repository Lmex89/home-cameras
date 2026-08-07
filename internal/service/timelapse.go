// Package service contains the application services that orchestrate
// repositories and infrastructure adapters. This file implements the
// timelapse rendering pipeline (frame annotation + ffmpeg assembly).
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fogleman/gg"
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/domain"
	"github.com/Lmex89/home-cameras/internal/repository"
)

// TimelapseService renders MP4 timelapses from snapshot frames, drawing
// YOLO detection boxes when analysis data is available. Mirrors the
// legacy TimelapseService (process pool + ffmpeg concat + faststart).
type TimelapseService struct {
	cfg   config.Config
	db    *sqlx.DB
	snaps *repository.SnapshotRepository
}

// NewTimelapseService builds the timelapse service.
//
// Args:
//
//	cfg: Application configuration (frame duration, workers, classes).
//	db: Shared database pool.
//	snaps: Snapshot repository for frame lookups.
//
// Returns:
//
//	A ready TimelapseService.
func NewTimelapseService(cfg config.Config, db *sqlx.DB, snaps *repository.SnapshotRepository) *TimelapseService {
	return &TimelapseService{cfg: cfg, db: db, snaps: snaps}
}

// RenderAnnotated builds an annotated timelapse MP4 for a camera/date
// using detection boxes for the configured object classes. Snapshots
// without analysis results are still included (no boxes drawn).
//
// Args:
//
//	ctx: Request context.
//	cameraID: The camera to build the video for.
//	day: The date of the snapshots.
//	classes: Set of class names to annotate (nil = config default).
//
// Returns:
//
//	The output MP4 path and the temp working directory.
//
// Raises:
//
//	Error: When no snapshots/files exist or ffmpeg fails.
func (s *TimelapseService) RenderAnnotated(ctx context.Context, cameraID int64, day time.Time, classes map[string]bool) (string, string, error) {
	if classes == nil {
		classes = s.cfg.TimelapseClassSet()
	}
	snaps, err := s.snaps.GetByCameraAndDate(ctx, cameraID, day)
	if err != nil {
		return "", "", err
	}
	if len(snaps) == 0 {
		return "", "", fmt.Errorf("no snapshots for camera %d on %s", cameraID, day.Format("2006-01-02"))
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].CapturedAt.Before(snaps[j].CapturedAt.Time) })
	log.Info().Int("snapshots", len(snaps)).Msg("annotated timelapse: snapshots found")

	// Fetch analysis detections per snapshot.
	analysesRepo := repository.NewSnapshotAnalysisRepository(s.db)
	detectionsBySnap := map[int64][]domain.Detection{}
	for _, sn := range snaps {
		analyses, err := analysesRepo.GetBySnapshot(ctx, sn.ID)
		if err != nil {
			continue
		}
		for i := range analyses {
			if analyses[i].ObjectsJSON == nil {
				continue
			}
			var dets []domain.Detection
			if err := json.Unmarshal([]byte(*analyses[i].ObjectsJSON), &dets); err == nil {
				detectionsBySnap[sn.ID] = dets
				break
			}
		}
	}

	tempDir, err := os.MkdirTemp("", fmt.Sprintf("atl_%d_", cameraID))
	if err != nil {
		return "", "", err
	}

	// Draw annotated frames in parallel (worker goroutines with a
	// semaphore, mirroring the legacy ProcessPoolExecutor).
	framePaths := make([]string, 0, len(snaps))
	workers := s.cfg.TimelapseWorkers
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, sn := range snaps {
		fullPath := filepath.Join(s.cfg.SnapshotsDir(), sn.ImagePath)
		if _, err := os.Stat(fullPath); err != nil {
			continue
		}
		outPath := filepath.Join(tempDir, fmt.Sprintf("%08d.jpg", sn.ID))
		wg.Add(1)
		sem <- struct{}{}
		go func(src, out string, dets []domain.Detection) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := drawFrame(src, out, dets, classes); err != nil {
				log.Warn().Err(err).Str("src", src).Msg("frame annotation failed")
				return
			}
			mu.Lock()
			framePaths = append(framePaths, out)
			mu.Unlock()
		}(fullPath, outPath, detectionsBySnap[sn.ID])
	}
	wg.Wait()

	if len(framePaths) == 0 {
		os.RemoveAll(tempDir)
		return "", "", fmt.Errorf("no valid image files found for camera %d on %s", cameraID, day.Format("2006-01-02"))
	}
	sort.Strings(framePaths)
	log.Info().Int("frames", len(framePaths)).Msg("frames annotated")

	outputPath := filepath.Join(tempDir, fmt.Sprintf("timelapse_annotated_%d_%s.mp4", cameraID, day.Format("2006-01-02")))
	if err := s.assemble(ctx, framePaths, outputPath, day); err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}
	sizeMB := float64(fileSizeBytes(outputPath)) / (1024 * 1024)
	log.Info().Float64("size_mb", sizeMB).Str("output", outputPath).Msg("annotated timelapse ready")
	return outputPath, tempDir, nil
}

// RenderPlain builds a plain (unannotated) timelapse from raw frames,
// used by the daily report video endpoint.
//
// Args:
//
//	ctx: Request context.
//	snaps: Snapshots (already filtered/sorted by the caller).
//	day: The date of the snapshots.
//
// Returns:
//
//	The output MP4 path and the temp working directory.
func (s *TimelapseService) RenderPlain(ctx context.Context, snaps []domain.Snapshot, day time.Time) (string, string, error) {
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("tl_%d_", day.Unix()))
	if err != nil {
		return "", "", err
	}
	framePaths := make([]string, 0, len(snaps))
	for i, sn := range snaps {
		src := filepath.Join(s.cfg.SnapshotsDir(), sn.ImagePath)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		// Copy to a sequential name so the concat demuxer keeps order.
		out := filepath.Join(tempDir, fmt.Sprintf("%08d.jpg", i))
		if err := copyFile(src, out); err != nil {
			log.Warn().Err(err).Str("src", src).Msg("frame copy failed")
			continue
		}
		framePaths = append(framePaths, out)
	}
	if len(framePaths) == 0 {
		os.RemoveAll(tempDir)
		return "", "", fmt.Errorf("no valid image files found for %s", day.Format("2006-01-02"))
	}
	sort.Strings(framePaths)
	outputPath := filepath.Join(tempDir, fmt.Sprintf("timelapse_%s.mp4", day.Format("2006-01-02")))
	if err := s.assemble(ctx, framePaths, outputPath, day); err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}
	log.Info().Str("output", outputPath).Msg("plain timelapse ready")
	return outputPath, tempDir, nil
}

// assemble runs ffmpeg to concatenate the frames into an MP4 with
// faststart. Uses the concat demuxer with per-frame durations so the
// video plays at the configured speed.
//
// Args:
//
//	ctx: Request context.
//	framePaths: Ordered list of JPEG frames.
//	outputPath: Where to write the final MP4.
//	day: Date used only for logging.
//
// Raises:
//
//	Error: When ffmpeg fails on either pass.
func (s *TimelapseService) assemble(ctx context.Context, framePaths []string, outputPath string, day time.Time) error {
	tempDir := filepath.Dir(outputPath)
	fileList := filepath.Join(tempDir, "files.txt")
	duration := fmt.Sprintf("%g", s.cfg.TimelapseFrameDuration)
	var sb strings.Builder
	for i, p := range framePaths {
		fmt.Fprintf(&sb, "file '%s'\n", p)
		if i < len(framePaths)-1 {
			fmt.Fprintf(&sb, "duration %s\n", duration)
		}
	}
	if err := os.WriteFile(fileList, []byte(sb.String()), 0o644); err != nil {
		return err
	}

	rawPath := filepath.Join(tempDir, "raw.mp4")
	cctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	if err := runFFmpeg(cctx,
		"-y", "-f", "concat", "-safe", "0", "-i", fileList,
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "28",
		"-pix_fmt", "yuv420p", rawPath,
	); err != nil {
		return fmt.Errorf("ffmpeg concat failed: %w", err)
	}
	if err := runFFmpeg(cctx,
		"-y", "-i", rawPath, "-c", "copy", "-movflags", "+faststart", outputPath,
	); err != nil {
		return fmt.Errorf("ffmpeg faststart failed: %w", err)
	}
	os.Remove(rawPath)
	return nil
}

// runFFmpeg executes ffmpeg with the given arguments, capturing stderr
// for error reporting.
//
// Args:
//
//	ctx: Bounded context.
//	args: ffmpeg arguments.
//
// Raises:
//
//	Error: When ffmpeg exits non-zero.
func runFFmpeg(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stderr := new(strings.Builder)
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// classColors maps object classes to their annotation colors (parity
// with the legacy CLASS_COLORS palette).
var classColors = map[string]color.RGBA{
	"person":     {255, 159, 10, 255}, // amber
	"car":        {90, 200, 250, 255}, // cyan
	"motorcycle": {52, 199, 89, 255},  // green
	"dog":        {255, 59, 48, 255},  // red
	"cat":        {255, 59, 48, 255},  // red
	"truck":      {175, 82, 222, 255}, // purple
	"bicycle":    {90, 200, 250, 255}, // cyan
	"bus":        {175, 82, 222, 255}, // purple
}

// defaultColor is used for classes without a dedicated color.
var defaultColor = color.RGBA{255, 255, 255, 255}

// drawFrame loads src, draws the detections of the allowed classes and
// saves the result to out (JPEG). Mirrors the legacy _draw_frame_worker.
//
// Args:
//
//	src: Source JPEG path.
//	out: Destination JPEG path.
//	detections: Detection boxes for this frame.
//	classes: Allowed class names (others are skipped).
//
// Returns:
//
//	Any decode/draw/encode error.
func drawFrame(src, out string, detections []domain.Detection, classes map[string]bool) error {
	img, err := loadImage(src)
	if err != nil {
		return err
	}
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	dc := gg.NewContextForImage(img)
	thickness := float64(clampInt(maxInt(2, minInt(width, height)/300), 2, 12))

	for _, d := range detections {
		if !classes[d.ClassName] {
			continue
		}
		if len(d.BBox) != 4 {
			continue
		}
		x1, y1, x2, y2 := d.BBox[0], d.BBox[1], d.BBox[2], d.BBox[3]
		col, ok := classColors[d.ClassName]
		if !ok {
			col = defaultColor
		}
		dc.SetColor(col)
		dc.SetLineWidth(thickness)
		dc.DrawRectangle(x1, y1, x2-x1, y2-y1)
		dc.Stroke()

		label := fmt.Sprintf("%s %.0f%%", d.ClassName, d.Confidence*100)
		fontFace := labelFont(float64(maxInt(width/80, 14)))
		dc.SetFontFace(fontFace)
		tw := measureTextWidth(fontFace, label)
		th := 20.0
		if tw == 0 {
			tw = float64(len(label)) * 8
		}
		dc.SetColor(col)
		dc.DrawRectangle(x1, y1-th-6, tw+8, th+6)
		dc.Fill()
		dc.SetColor(color.RGBA{255, 255, 255, 255})
		dc.DrawString(label, x1+4, y1-4)
	}
	return dc.SavePNG(out)
}

// loadImage decodes a JPEG/PNG file into an RGBA image.
//
// Args:
//
//	path: Image file path.
//
// Returns:
//
//	The decoded image.
func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// labelFont loads the DejaVu Sans Bold font when available, falling
// back to the built-in basic font (parity with the legacy Pillow font
// handling).
//
// Args:
//
//	size: Font size in points.
//
// Returns:
//
//	A font.Face ready for drawing.
func labelFont(size float64) font.Face {
	paths := []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
		"/usr/share/fonts/dejavu/DejaVuSans-Bold.ttf",
	}
	for _, p := range paths {
		if f, err := gg.LoadFontFace(p, size); err == nil {
			return f
		}
	}
	return basicfont.Face7x13
}

// measureTextWidth approximates the pixel width of a label.
//
// Args:
//
//	f: The font face.
//	s: The text.
//
// Returns:
//
//	The width in pixels.
func measureTextWidth(f font.Face, s string) float64 {
	adv := fixed.Int26_6(0)
	for _, r := range s {
		a, ok := f.GlyphAdvance(r)
		if ok {
			adv += a
		}
	}
	return float64(adv) / 64.0
}

// copyFile copies src to dst (used for plain timelapse frame staging).
//
// Args:
//
//	src: Source path.
//	dst: Destination path.
//
// Returns:
//
//	Any I/O error.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// fileSizeBytes returns the size of a file (0 when it cannot be read).
func fileSizeBytes(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
