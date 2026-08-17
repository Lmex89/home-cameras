// Package main provides a CLI tool that drains the analysis job queue
// by processing all pending jobs in a tight loop with the real YOLO engine.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/database"
	"github.com/Lmex89/home-cameras/internal/infrastructure/ml"
	"github.com/Lmex89/home-cameras/internal/repository"
	"github.com/Lmex89/home-cameras/internal/service"
)

func main() {
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("config")
	}
	db, err := database.Open(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("db")
	}
	defer db.Close()

	detector := ml.NewDetector(cfg)
	svc := service.NewAnalysisService(cfg, db, detector,
		repository.NewAnalysisJobRepository(db),
		repository.NewSnapshotRepository(db),
		repository.NewSnapshotAnalysisRepository(db),
		repository.NewCameraRepository(db))

	total := 0
	start := time.Now()

	for {
		n, err := svc.ProcessNextBatch(ctx, 200)
		if err != nil {
			log.Error().Err(err).Msg("batch failed")
			continue
		}
		total += n
		if n == 0 {
			break
		}
		elapsed := time.Since(start).Round(time.Second)
		rate := float64(total) / time.Since(start).Seconds()
		fmt.Printf("\r  processed: %d | %.1f jobs/s | %s elapsed", total, rate, elapsed)
	}

	fmt.Printf("\nDone: %d jobs in %s (%.1f jobs/s)\n",
		total, time.Since(start).Round(time.Second),
		float64(total)/time.Since(start).Seconds())
}
