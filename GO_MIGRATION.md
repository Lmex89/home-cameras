# Go Port — Migration Guide (Python → Go)

This directory (`cmd/`, `internal/`) is the Go port of the Python
application. It implements the same feature set with the same database
schema, so it can be deployed against an existing `data/cameras.db`
without data migration.

## File mapping (read this first)

| Python (legacy) | Go (port) |
|---|---|
| `app/core/config.py` | `internal/config/config.go` |
| `app/core/database.py` | `internal/database/database.go` (schema embedded via `//go:embed`) |
| `app/core/unit_of_work.py` | **Gone** — Go passes `*sqlx.Tx` into repositories (`repository.DBTX`) |
| `app/domain/models.py` | `internal/domain/models.go` |
| `app/domain/schemas.py` | `internal/domain/schemas.go` (request/response DTOs) |
| `app/application/repositories/*.py` | `internal/repository/*.go` |
| `app/application/services/*.py` | `internal/service/*.go` |
| `app/infrastructure/onvif.py` | `internal/infrastructure/onvif/onvif.go` |
| `app/infrastructure/storage.py` | `internal/infrastructure/storage/s3.go` |
| `app/infrastructure/telegram.py` | `internal/infrastructure/telegram/telegram.go` |
| `app/infrastructure/archive.py` | `internal/infrastructure/archive/archive.go` |
| `app/infrastructure/ml/yolo.py` | `internal/infrastructure/ml/` (`stub.go` default, `engine_opencv.go` behind `-tags opencv`) |
| `app/api/routers/*.py` | `internal/api/*.go` (chi handlers) |
| `app/scheduler.py` | `internal/scheduler/scheduler.go` |
| `app/seed.py` | `internal/seed/seed.go` |
| `app/main.py` (lifespan) | `cmd/server/main.go` (wiring) |
| `app/web/templates/`, `app/web/static/`, `index.html` | `internal/web/static/` (embedded) |

## Python developer cheat-sheet

- **Async → goroutines.** `async def` maps to plain functions; blocking
  calls (HTTP, ffmpeg, ONVIF SOAP) just run inline — the Go runtime
  handles concurrency. `asyncio.to_thread` wrappers are unnecessary.
- **Transactions.** Instead of a UnitOfWork context manager, begin with
  `db.BeginTxx(ctx, nil)` and build repositories over the `*sqlx.Tx`.
  `defer tx.Rollback()` + explicit `tx.Commit()` replaces the
  `__aexit__` semantics.
- **Time.** SQLite stores `YYYY-MM-DD HH:MM:SS` strings. `domain.SQLTime`
  scans/binds that format and interprets naive values in `time.Local`
  (set from `TIMEZONE` at startup). Comparisons in SQL are lexicographic
  and therefore chronological.
- **Validation.** Pydantic constraints are enforced manually in the
  handlers (`internal/api/cameras.go` shows the pattern). The `domain.Day`
  type accepts `"YYYY-MM-DD"` like the old `date` fields.
- **Logging.** `loguru` → `zerolog`: `log.Info().Str("camera", name).Msg(...)`.
  `logger.exception` → `log.Error().Err(err).Msg(...)`.
- **Settings.** `pydantic-settings` → `caarlos0/env` + a tiny `.env`
  loader in `config.Load()`. Env var names are unchanged.

## Build & run

```bash
make build        # bin/cameras-go
make run          # dev server, data dir ./data
make test         # go test -race -cover
make docker       # docker build -t cameras-go .
```

Run from the project root so `cameras.yaml` and `.env` resolve (or point
`DATA_DIR` elsewhere).

## Object detection

The default build compiles without OpenCV and runs the detector in stub
mode (no detections), like the legacy app without `ultralytics`. For
native inference:

1. Export the model once: `python scripts/export_yolo_onnx.py` →
   `models/yolov8n.onnx`.
2. Install OpenCV dev headers.
3. Build with `make opencv` (or `go build -tags opencv`).

The engine parses the ultralytics NCHW `1x84x8400` output layout.

## Notes on parity

- The DB schema and the legacy migrations (`interval_minutes` rename,
  `archive_path` column, analysis tables) are applied on open.
- ZIP archives use `zipfile`-compatible append semantics (rewrite
  through a temp file + rename) and the same `zip::filename` references.
- TLS verification is disabled for camera fetches (self-signed certs).
- Camera capture intervals are anchored to the last successful snapshot
  so schedules survive restarts, matching `load_schedule()`.
