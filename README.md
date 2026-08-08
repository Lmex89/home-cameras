# Camera Monitor (Go)

[![Go Version](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
[![Build](https://img.shields.io/badge/build-make-0f9d58)](Makefile)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Code Graph](https://img.shields.io/badge/codegraph-indexed-7c3aed)](https://github.com/isink17/codegraph)

ONVIF-compatible camera snapshot monitoring system with local ML analysis. Periodically
captures snapshots from IP cameras, runs YOLO object detection, flags unusual events for
human review, generates annotated daily timelapse videos, and ships a self-contained
web dashboard. Single static Go binary — no CGO, no Python, no service mesh.

This repository is a complete Go port of a prior Python/FastAPI implementation. It
preserves the SQLite schema and env-var names, but ships as one binary and removes the
runtime dependencies on Python, FastAPI, SQLAlchemy and APScheduler.

---

## Table of contents

- [Why?](#why)
- [Features](#features)
- [Quick start](#quick-start)
- [How it works](#how-it-works)
- [Configuration reference](#configuration-reference)
- [Defining cameras (`cameras.yaml`)](#defining-cameras-camerasyaml)
- [Snapshot capture strategy](#snapshot-capture-strategy)
- [HTTP API](#http-api)
- [Web dashboard](#web-dashboard)
- [ML analysis & review rules](#ml-analysis--review-rules)
- [Timelapse videos](#timelapse-videos)
- [Retention & purge](#retention--purge)
- [Data directory layout](#data-directory-layout)
- [Project layout](#project-layout)
- [Development workflow](#development-workflow)
- [Testing](#testing)
- [Docker](#docker)
- [Telegram & S3 setup](#telegram--s3-setup)
- [Troubleshooting](#troubleshooting)
- [Migrating from the Python app](#migrating-from-the-python-app)
- [Security notes](#security-notes)
- [Contributing](#contributing)
- [License](#license)

---

## Why?

Home / small-business camera setups usually outgrow the vendor's cloud quickly: you want
**local storage you control**, **automated review of routine events** (a person in the
backyard at 3 a.m. shouldn't need you to scrub a 12-hour timeline), and a **reliable
daily digest** of what happened.

This service runs entirely on a single host:

- pulls snapshots from any ONVIF-compatible camera (or via direct URL / RTSP fallback),
- runs a small YOLO model locally to detect people, vehicles and other objects,
- flags suspicious events for human review,
- archives the rest for `SNAPSHOT_RETENTION_DAYS`,
- and produces a daily annotated timelapse you can review on your phone.

No cloud lock-in. No vendor telemetry. One binary, one SQLite file, one volume.

## Features

| Area | What you get |
| --- | --- |
| **ONVIF client** | Connects to cameras via ONVIF, auto-selects a profile, falls back to a configurable `snapshot_url`, and finally to RTSP+ffmpeg if the camera doesn't expose `GetSnapshotUri`. |
| **Scheduled capture** | Per-camera interval (default 60 s) with **restart-anchored** timers — the schedule survives process restarts. |
| **Local ML detection** | YOLO runs in-process; default build uses a **stub detector** (no OpenCV needed), opt into native gocv inference with `make opencv`. |
| **Review rule engine** | Auto-flags persons after hours, high crowd counts, and unexpected object classes; surfaces them in the dashboard and `GET /api/reviews/pending`. |
| **Annotated timelapse** | Daily MP4 with YOLO bounding boxes, rendered with `gg` + `golang.org/x/image`, then optionally uploaded to S3-compatible storage and pushed to Telegram. |
| **Telegram notifier** | Sends timelapses, sends health alarms when a camera goes stale, and posts on scheduled-job panics. >50 MB videos are sent as a text+URL link instead. |
| **S3-compatible storage** | First-class support for Backblaze B2 (and any S3-compatible endpoint) via `minio-go`. |
| **SQLite everywhere** | `modernc.org/sqlite` (pure Go, no CGO). WAL + `busy_timeout` PRAGMAs out of the box. The whole DB is one file you can `cp` for backup. |
| **Single static binary** | `make build` produces a `bin/cameras-go` you can `scp` to a server. `docker compose up -d --build` runs the whole service in one container. |
| **Embedded SPA** | The web dashboard is `//go:embed`-ed in the binary — no separate static file server. |
| **YAML seed** | Cameras are idempotently seeded from `cameras.yaml` on every boot, so config is in version control. |

## Quick start

### 1. Clone & enter

```bash
git clone <your fork> home-cameras
cd home-cameras
```

### 2. Configure

```bash
cp .env.example .env             # tweak .env (TZ, port, Telegram, S3 keys)
cp cameras.example.yaml cameras.yaml  # add your cameras
$EDITOR cameras.yaml .env
```

### 3. Run

```bash
make run                         # ensures requirements, builds and launches on :8004
# or
make build && ./bin/cameras-go
```

Open <http://localhost:8004> — the dashboard is served from the same binary.

`make run` first runs `make requirements`, which:

- downloads the YOLOv8 ONNX model into `models/` when missing,
- verifies `ffmpeg` is on `$PATH`,
- checks for OpenCV dev headers — when present, the gocv engine is compiled in and
  detection runs natively; otherwise the binary builds with the stub detector and a
  warning explains how to install OpenCV.

### 4. (Optional) Enable real YOLO detection

The default build uses a **stub detector** that records empty analyses. To get real
detections, install OpenCV once and rebuild with the gocv engine:

```bash
# One-time: install OpenCV dev headers (Debian/Ubuntu; macOS: brew install opencv)
sudo apt install -y libopencv-dev pkg-config

# Download the model (or use scripts/export_yolo_onnx.py yolov8n.pt models/)
make model

# Build with the gocv YOLO engine and run
make opencv
./bin/cameras-go
```

`make requirements` (run automatically by `make run`) prints the same checklist and
can be re-run anytime to verify the setup.

### 5. Verify

```bash
curl http://localhost:8004/api/healthz
# {"database":true,"status":"ok","storage":false,"telegram":false,"time":"..."}

curl http://localhost:8004/api/cameras
# [...]
```

## How it works

The service runs four concurrent workloads inside one process:

```
                 ┌────────────────────────────────────────────────┐
   cameras.yaml ─┤ seed.FromYAML (idempotent on every boot)       │
                 └────────────────────────────────────────────────┘
                                       │
                                       ▼
                 ┌────────────────────────────────────────────────┐
   per-camera    │ scheduler: interval timer (restart-anchored)   │
   IntervalTrigger captureJob → SnapshotService.Capture           │
                 │   (direct URL → ONVIF GetSnapshotUri → RTSP)   │
                 └────────────────────────────────────────────────┘
                                       │
                          ┌────────────┴────────────┐
                          ▼                         ▼
             ┌────────────────────┐     ┌────────────────────┐
             │ data/snapshots/... │     │ analysis_jobs      │
             └────────────────────┘     └────────────────────┘
                                                  │
                                                  ▼
   every ANALYSIS_INTERVAL_SECONDS:
             ┌────────────────────────────────────────────────┐
             │ scheduler.analysisTick → AnalysisService        │
             │  → ml.Detector (stub | gocv YOLO)               │
             │  → applyReviewRules                             │
             │  → snapshot_analyses row                        │
             └────────────────────────────────────────────────┘

   every day @ 06:00 (configurable):
             ┌────────────────────────────────────────────────┐
             │ retention.Run: zip + delete old snapshots/videos│
             └────────────────────────────────────────────────┘

   every day @ TIMELAPSE_HOUR (default 21:00):
             ┌────────────────────────────────────────────────┐
             │ TimelapseService.RenderAnnotated                │
             │  → upload to S3 (optional)                      │
             │  → send via Telegram (or text+URL if >50 MB)    │
             └────────────────────────────────────────────────┘

   every HEALTH_CHECK_INTERVAL_MINUTES:
             ┌────────────────────────────────────────────────┐
             │ healthJob: raise Telegram alarm on stale cameras│
             └────────────────────────────────────────────────┘
```

Key properties of the design:

- **Restart-anchored timers.** The first capture after boot is scheduled relative to
  the camera's last successful snapshot (or `now()` for new cameras). This means a
  09:00, 09:01, 09:02 … schedule doesn't drift to 10:00, 11:00, 12:00 after a restart.
- **Panic-safe jobs.** Every scheduled callback runs through `scheduler.guard`, which
  recovers panics, logs them, and posts a Telegram alarm. One bad analysis run can't
  kill the process.
- **SQLite write contention mitigation.** Retention pauses capture/analysis jobs
  before opening a long write transaction, then resumes them. This avoids
  `database is locked` errors on cheap disks.
- **Archive references are in-DB.** When a raw JPEG is zipped, the row's
  `archive_path` becomes `data/archives/.../file.zip::filename.jpg`. The HTTP layer
  transparently serves from disk or ZIP based on what's available.

## Configuration reference

Configuration is loaded from a `.env` file in the working directory, then overridden by
real environment variables. All settings have safe defaults; the only required values
are the Telegram bot token / chat ID and S3 keys *if* you enable those integrations.

### Core

| Variable | Default | Description |
| --- | --- | --- |
| `APP_NAME` | `Camera Monitor` | Display name in the dashboard. |
| `DEBUG` | `true` | Verbose zerolog output (request log, scheduler traces). |
| `HOST` | `0.0.0.0` | Bind address. |
| `PORT` | `8004` | HTTP port. |
| `TIMEZONE` | `America/Mexico_City` | IANA timezone. All timestamps, cron triggers, and `Day` parsing use this zone. |
| `DATA_DIR` | `./data` | Root for snapshots, videos, archives, logs, models, and the SQLite file. |

### Capture

| Variable | Default | Description |
| --- | --- | --- |
| `DEFAULT_INTERVAL_SECONDS` | `60` | Fallback when a camera in `cameras.yaml` doesn't specify one. |
| `CAPTURE_TIMEOUT_SECONDS` | `120` | Maximum time for one capture attempt (direct URL / ONVIF / RTSP). |
| `HEALTH_CHECK_INTERVAL_MINUTES` | `10` | Stale-camera check frequency; raises a Telegram alarm when no snapshot has been written within `2 × CAPTURE_TIMEOUT_SECONDS`. |

### Retention

| Variable | Default | Description |
| --- | --- | --- |
| `SNAPSHOT_RETENTION_DAYS` | `30` | Delete snapshot rows + orphan archives older than this. |
| `SNAPSHOT_ZIP_AFTER_DAYS` | `7` | Zip raw JPEGs older than this into `data/archives/`. |
| `VIDEO_RETENTION_DAYS` | `30` | Delete video rows + archives older than this. |

### ML analysis

| Variable | Default | Description |
| --- | --- | --- |
| `ANALYSIS_ENABLED` | `true` | Master switch for the analysis pipeline. |
| `ANALYSIS_INTERVAL_SECONDS` | `30` | Polling frequency for pending `analysis_jobs`. |
| `YOLO_MODEL_PATH` | `models/yolov8n.pt` | Path to the model. `.pt` works with the opencv engine; `.onnx` works with `scripts/export_yolo_onnx.py`. `make model` downloads the `.onnx` next to it. |
| `YOLO_CONFIDENCE_THRESHOLD` | `0.5` | Minimum confidence to record a detection. |
| `YOLO_NUM_THREADS` | `4` | Max CPU threads used by YOLO inference (`cv::setNumThreads`). |
| `REVIEW_PERSON_AFTER_HOUR` | `22` | Person detections **after** this hour (24h) are flagged. |
| `REVIEW_PERSON_BEFORE_HOUR` | `6` | Person detections **before** this hour (24h) are flagged. |
| `REVIEW_MAX_PERSON_COUNT` | `5` | Person count above this in a single snapshot is flagged. |

### Timelapse

| Variable | Default | Description |
| --- | --- | --- |
| `TIMELAPSE_ENABLED` | `true` | Master switch for the daily timelapse cron. |
| `TIMELAPSE_HOUR` | `21` | Hour (0-23) the daily timelapse runs. |
| `TIMELAPSE_MINUTE` | `0` | Minute (0-59) the daily timelapse runs. |
| `TIMELAPSE_CAMERA_ID` | `6` | Camera ID used for the daily annotated timelapse. |
| `TIMELAPSE_OBJECT_CLASSES` | `person,car,motorcycle` | Comma-separated YOLO classes to draw bounding boxes for. |
| `TIMELAPSE_FRAME_DURATION` | `0.4675` | Seconds per frame in the output MP4. |
| `TIMELAPSE_WORKERS` | `3` | Parallel workers used to render annotated frames. |

### Telegram

| Variable | Default | Description |
| --- | --- | --- |
| `TELEGRAM_ENABLED` | `false` | Enable Telegram notifications. |
| `TELEGRAM_BOT_TOKEN` | `""` | Bot token from `@BotFather`. |
| `TELEGRAM_CHAT_ID` | `""` | Target chat / channel ID. |

### S3-compatible storage

| Variable | Default | Description |
| --- | --- | --- |
| `STORAGE_ENABLED` | `false` | Enable uploads (used for timelapse videos >50 MB and as a Telegram link target). |
| `STORAGE_ENDPOINT_URL` | `""` | S3 endpoint (e.g. `https://s3.us-east-005.backblazeb2.com`). |
| `STORAGE_BUCKET_NAME` | `""` | Bucket name. |
| `STORAGE_ACCESS_KEY` | `""` | Access key ID. |
| `STORAGE_SECRET_KEY` | `""` | Secret access key. |
| `STORAGE_PUBLIC_URL` | `""` | Public base URL for the bucket (used to build share links). |
| `STORAGE_REGION` | `us-west-004` | S3 region. |

## Defining cameras (`cameras.yaml`)

Cameras are seeded from `cameras.yaml` in the working directory on every boot. The
seeder is **idempotent**: cameras present in both YAML and DB are updated, new ones
are inserted, and cameras missing from the YAML are deleted from the DB.

```yaml
cameras:
  - name: "Patio Trasero"
    host: "192.168.1.100"
    port: 80
    username: "admin"
    password: "your_password_here"
    interval_seconds: 60        # omit to inherit DEFAULT_INTERVAL_SECONDS
    enabled: true

  - name: "Puerta Principal"
    host: "192.168.1.101"
    port: 8899
    username: "admin"
    password: "your_password_here"
    interval_seconds: 1800      # every 30 min
    enabled: true

  - name: "Cochera (snapshot URL override)"
    host: "192.168.1.102"
    port: 80
    username: "admin"
    password: "secret"
    snapshot_url: "http://192.168.1.102/cgi-bin/snapshot.cgi"  # bypasses ONVIF
    interval_seconds: 300
    enabled: true
```

`profile_token` is optional and is passed to the ONVIF client when the camera's default
profile isn't what you want. `snapshot_url` short-circuits capture entirely (the first
strategy that succeeds wins; see below).

The `cameras.example.yaml` in the repo has the same shape with placeholders — copy it
to `cameras.yaml` and edit.

## Snapshot capture strategy

For every tick of a camera's interval, the service tries the following in order, and
keeps the first success:

1. **Direct URL** — if `snapshot_url` is set on the camera, `GET` that URL with HTTP
   Basic Auth. This is the fastest path and works for cameras that expose a vendor
   snapshot CGI.
2. **ONVIF `GetSnapshotUri`** — the standard ONVIF approach. The service auto-selects
   the highest-resolution H.264 profile and asks the camera for its snapshot URI.
3. **RTSP + ffmpeg** — fallback. We resolve the RTSP stream URI via ONVIF, then
   `exec ffmpeg` to grab a single frame as JPEG. Requires `ffmpeg` on `$PATH`; the
   `opencv` Docker image bundles it.

The first strategy that returns a valid JPEG wins. If all three fail, the snapshot row
is recorded with `status = 'error'` and an `error_message` describing the last failure.

## HTTP API

All endpoints are mounted under `/api/`. The web dashboard is at `/` and `/reviews`
(redirects to `/reviews.html`).

### Cameras

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/cameras` | List cameras with last-snapshot metadata (dashboard payload). |
| `POST` | `/api/cameras` | Create a camera. |
| `POST` | `/api/cameras/test` | Probe an ONVIF endpoint without persisting. |
| `GET` | `/api/cameras/{id}` | Single camera. |
| `PUT` | `/api/cameras/{id}` | Partial update; reschedules the capture job. |
| `DELETE` | `/api/cameras/{id}` | Remove; also stops the capture job. |
| `POST` | `/api/cameras/{id}/snapshot` | Force an out-of-schedule capture. |

### Snapshots

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/snapshots/{id}` | Snapshot metadata + analysis. |
| `GET` | `/api/snapshots/{camera_id}/by-date?snapshot_date=YYYY-MM-DD` | All snapshots for a camera on a date. |
| `GET` | `/api/snapshots/image/{id}` | Raw JPEG (falls back to ZIP archive). |
| `GET` | `/api/data/manifest.json` | Dashboard manifest (last snapshot, counts, review queue). |
| `GET` | `/api/report/{date}` | Daily report. |
| `GET` | `/api/report/{date}/video/{camera_id}` | Stream a plain timelapse MP4 for the day. |

### Videos

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/api/videos` | Render a plain timelapse for `camera_id` + `date` (+ optional `hour`). |
| `POST` | `/api/videos/annotated` | Render an **annotated** timelapse, upload to S3, notify Telegram. |
| `GET` | `/api/videos/download/{filename}` | Stream a generated or archived MP4. |

### Reviews

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/reviews/pending` | Snapshots flagged for human review. |
| `GET` | `/api/reviews/count` | Queue size. |
| `GET` | `/api/reviews/detections?days_back=1&camera_id=&class_name=&limit=500&offset=0&date_from=` | Paginated detection browser. |
| `POST` | `/api/reviews/bulk-review` | Mark many analyses at once. |
| `POST` | `/api/reviews/{id}/review` | Confirm or reject a single flag. |

### Retention

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/api/retention/run` | Trigger the daily retention pipeline on demand. Pauses capture/analysis while it runs. |
| `POST` | `/api/retention/purge` | **Destructive.** Deletes everything older than `{"days": N}` without archiving. |

### Misc

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/healthz` | DB ping + flags. Returns `200` even if some integrations are disabled. |
| `GET` | `/api/healthz` | Alias of `/healthz`. |
| `GET` | `/snapshots/*` | Static raw snapshot files (path-traversal safe). |

## Web dashboard

The repository ships a single-page dashboard embedded in the binary
(`internal/web/static/index.html` + `app.js` + `app.css`). It is intentionally
self-contained — no SPA framework, no CDNs, no external assets.

Two pages are served:

- **`/`** — overview of every camera: latest snapshot, total count, capture status,
  and review queue badge. The "Force snapshot" and "Settings" actions hit the JSON
  API.
- **`/reviews`** — review queue: every analysis flagged for human review, with
  one-click confirm/reject and bulk actions.

The frontend is plain ES modules. Open the browser dev tools for the network log; the
JSON contracts above are the only API it uses.

## ML analysis & review rules

After a successful capture, the snapshot service enqueues an `analysis_job`. A
scheduler tick (every `ANALYSIS_INTERVAL_SECONDS`, default 30 s) drains the queue in
batches of up to 5.

The detector is an interface:

```go
type Detector interface {
    Available() bool
    Detect(ctx context.Context, imagePath string) ([]Detection, error)
}
```

There are two implementations:

- **Stub** (`internal/infrastructure/ml/stub.go`) — always returns no detections. Used
  in the default build, in tests, and when no model file is configured. The pipeline
  stays correct end-to-end (rows are written, the review engine still runs against
  empty detections), so you can deploy the binary and watch the dashboard work
  before wiring up a model.
- **gocv YOLO** (`internal/infrastructure/ml/engine_opencv.go`, behind `-tags opencv`)
  — loads an ONNX export of YOLOv8 (`yolov8n.onnx` or similar) via `gocv` and parses
  the NCHW `1×84×8400` output tensor into `[]Detection`. Build with `make opencv`.

`applyReviewRules` then sets `review_required = true` and a `review_reason` when:

- the capture time is in `[REVIEW_PERSON_BEFORE_HOUR, REVIEW_PERSON_AFTER_HOUR)` and
  at least one person is detected, or
- the person count exceeds `REVIEW_MAX_PERSON_COUNT`, or
- an "unexpected" class appears (rule is currently permissive and logs; extend
  `applyReviewRules` in `internal/service/analysis.go` for custom rules per camera).

Flagged rows are surfaced in:

- the dashboard manifest (`/api/data/manifest.json`),
- `GET /api/reviews/pending`,
- the dedicated `/reviews` page.

## Timelapse videos

Two flavours:

1. **Plain** (`POST /api/videos`) — `ffmpeg` concat over the day's raw JPEGs, no
   overlays. Fast (a couple seconds per minute of output).
2. **Annotated** (`POST /api/videos/annotated`) — the same flow, but every frame is
   decoded with `golang.org/x/image`, YOLO bounding boxes are drawn with
   `fogleman/gg` for the configured classes, and the result is re-encoded. Slow but
   useful as a daily digest.

The daily cron job at `TIMELAPSE_HOUR:TIMELAPSE_MINUTE` (defaults 21:00) generates the
**annotated** timelapse for the **previous day** for `TIMELAPSE_CAMERA_ID`:

```
ffmpeg -framerate 1/<TIMELAPSE_FRAME_DURATION> -i frames/%05d.png \
       -c:v libx264 -pix_fmt yuv420p -movflags +faststart out.mp4
```

If `STORAGE_ENABLED = true`, the file is uploaded and the public URL is preferred
in the Telegram caption. If the file is >50 MB, the notifier sends a text+URL link
instead of the binary (Telegram's bot API cap is 50 MB).

Manual trigger:

```bash
curl -X POST -H "Content-Type: application/json" \
     -d '{"camera_id": 5, "date": "2026-08-07"}' \
     http://localhost:8004/api/videos/annotated
```

## Retention & purge

Two distinct jobs. Both pause capture/analysis while they run to avoid SQLite
write contention.

### Retention (`POST /api/retention/run` or daily 06:00 cron)

1. **Zip** — raw JPEGs older than `SNAPSHOT_ZIP_AFTER_DAYS` (default 7) are compressed
   into per-camera, per-day ZIP archives under `data/archives/`. The raw file is
   deleted and the row's `archive_path` becomes `data/archives/...zip::filename.jpg`.
   Snapshots whose raw file is already missing get a `<missing>` sentinel so the
   retention loop doesn't re-process them.
2. **Delete** — DB rows and orphan archives older than `SNAPSHOT_RETENTION_DAYS` /
   `VIDEO_RETENTION_DAYS` (default 30) are removed. A ZIP is only deleted once every
   snapshot referencing it has been expired.

### Purge (`POST /api/retention/purge`)

Destructive. Deletes raw snapshots, analysis records, analysis jobs, videos, and
archives older than `{"days": N}` **without** creating archives. Use this when you
want to reclaim disk space immediately. There is no undo.

```bash
curl -X POST -H "Content-Type: application/json" \
     -d '{"days": 3}' http://localhost:8004/api/retention/purge
```

## Data directory layout

All runtime data lives under `DATA_DIR` (default `./data`). This directory is
gitignored and intended to be a Docker volume in production.

```
data/
├── cameras.db              # SQLite — the entire application state
├── snapshots/
│   └── {camera_id}/
│       └── {YYYY}/{MM}/{DD}/{HHMMSS}.jpg
├── videos/
│   └── timelapse_{camera_id}_{YYYY-MM-DD}.mp4
│   └── timelapse_annotated_{camera_id}_{YYYY-MM-DD}.mp4
├── archives/
│   ├── snapshots/{camera_id}/{YYYY-MM-DD}.zip
│   └── videos/{camera_id}/{YYYY-MM-DD}.zip
├── models/                 # YOLO weights (.pt or .onnx)
└── logs/
    └── app_{YYYY-MM-DD}.log    # daily rotating zerolog file
```

## Project layout

```
home-cameras/
├── cmd/
│   └── server/
│       └── main.go         # wiring: config → log → DB → services → scheduler → HTTP
├── internal/
│   ├── api/                # chi router + handlers; validation lives here
│   │   ├── server.go       # router, middleware (request id, CORS, logging, recover)
│   │   ├── cameras.go      # /api/cameras/*
│   │   ├── snapshots.go    # /api/snapshots/*  + manifest
│   │   ├── reviews.go      # /api/reviews/*
│   │   ├── videos.go       # /api/videos/*
│   │   ├── report.go       # /api/report/* and /api/data/manifest.json
│   │   ├── retention.go    # /api/retention/*
│   │   ├── pages.go        # /, /reviews.html, /snapshots/*
│   │   └── helpers.go      # JSON, error mapping, path parsing
│   ├── config/             # caarlos0/env + .env loader; derives filesystem paths
│   ├── database/           # sqlx + pure-Go SQLite; //go:embed schema.sql
│   ├── domain/
│   │   ├── models.go       # entities + SQLTime (SQLite text ↔ time, ISO-8601 JSON)
│   │   ├── models_test.go
│   │   └── schemas.go      # request/response DTOs, Day ("YYYY-MM-DD")
│   ├── repository/         # 4 repos over DBTX (*sqlx.DB or *sqlx.Tx)
│   │   ├── camera.go
│   │   ├── snapshot.go
│   │   ├── snapshot_analysis.go
│   │   ├── analysis_job.go
│   │   ├── repository.go
│   │   └── repository_test.go
│   ├── service/            # business logic
│   │   ├── camera.go
│   │   ├── snapshot.go     # capture pipeline + daily video
│   │   ├── analysis.go     # detector + review rules
│   │   ├── retention.go    # zip + delete; destructive purge
│   │   └── timelapse.go    # annotated MP4 render
│   ├── infrastructure/
│   │   ├── onvif/          # use-go/onvif adapter
│   │   ├── ml/             # Detector interface
│   │   │   ├── ml.go
│   │   │   ├── stub.go     # always-empty
│   │   │   ├── engine_default.go
│   │   │   └── engine_opencv.go   # build tag: opencv
│   │   ├── telegram/       # 50 MB cap + S3 fallback
│   │   ├── storage/        # minio-go S3 client
│   │   └── archive/        # ZIP reference reader
│   ├── scheduler/          # per-camera timers + robfig/cron jobs + panic guard
│   ├── seed/               # cameras.yaml → DB idempotent sync
│   └── web/
│       ├── web.go          # //go:embed static/
│       └── static/
│           ├── index.html
│           ├── reviews.html
│           ├── css/
│           └── js/
├── scripts/
│   └── export_yolo_onnx.py # one-time .pt → .onnx conversion helper
├── data/                   # gitignored; see "Data directory layout"
├── .agents/skills/         # AI-assistant skills (accessibility, frontend-design, …)
├── .codegraph/             # local code-index cache (do not commit)
├── cameras.example.yaml
├── .env.example
├── Dockerfile
├── Makefile
├── AGENTS.md               # contributor / AI-assistant rules
├── go.mod / go.sum
└── README.md
```

The data flow is strict: **handlers → services → repositories**. There is no global
state; everything is constructed in `cmd/server/main.go` and passed via constructor
injection. Services open `*sqlx.Tx` and pass the transaction to repositories when
they need atomicity.

## Development workflow

### Prerequisites

- **Go 1.26+** (`go version` to check).
- **`ffmpeg`** on `$PATH` (for RTSP capture and timelapse assembly). `make requirements`
  checks for it on every start.
- (Optional) **OpenCV dev headers** for the gocv engine — `apt install libopencv-dev`
  on Debian/Ubuntu, `brew install opencv` on macOS. `make requirements` reports when
  they're missing (stub detector until installed).
- (Optional) **`air`** for live reload: `go install github.com/air-verse/air@latest`.
- (Optional) **`codegraph`** for code intelligence (used by the `AGENTS.md`
  instructions): `go install github.com/isink17/codegraph/cmd/codegraph@latest`.

### Build matrix

| Target | Command | Notes |
| --- | --- | --- |
| Default binary | `make build` | `bin/cameras-go`, no CGO, stub detector. |
| Run locally | `make run` | Ensures requirements (model download, ffmpeg/OpenCV check), builds (gocv engine when OpenCV is installed) and runs against `./data`. |
| Check requirements | `make requirements` | Downloads the YOLO model when missing, verifies `ffmpeg` and OpenCV headers, prints install steps. |
| Fetch YOLO model | `make model` | Downloads `models/yolov8n.onnx` (official ultralytics asset) unless already present. |
| Live reload | `make dev` | Wraps `air` (live reload), also ensures requirements. |
| Format | `make fmt` | `gofmt -w cmd internal`. |
| Vet | `make vet` | `go vet ./...`. |
| Tests | `make test` | `go test ./... -race -cover`. |
| gocv build | `make opencv` | `CGO_ENABLED=1 go build -tags opencv`. Requires OpenCV (fails fast with install steps). |
| Docker image | `make docker-up` | `docker compose` — alpine runtime with ffmpeg by default; `opencv` overlay adds native YOLO. |
| Clean | `make clean` | `rm -rf bin`. |

### Code style

This project follows the rules in `AGENTS.md`. Highlights:

- **SOLID** — one reason to change per type; `*sqlx.DB` is injected, never a global;
  the `ml.Detector` interface is small (`Available()` + `Detect()`).
- **Conventional Commits** — `<type>(<scope>): <description>`.
- **zerolog** — `log.Info().Int64("camera_id", id).Msg("…")`, never `fmt.Println`.
- **Google-style doc comments** — every exported symbol has a docstring starting with
  the symbol name and `Args:` / `Returns:` / `Raises:` sections where applicable.
- **Validation in handlers** — DTOs carry `json` tags, handlers do the bound checks
  (port range, `interval_seconds >= 10`, date format, pagination caps).

### Code navigation

The repository is indexed by [codegraph](https://github.com/isink17/codegraph). The
MCP server is configured in `.mcp.json` and starts automatically with OpenCode. To
use the CLI directly:

```bash
codegraph index .                                 # re-index after Go changes
codegraph find-symbol . "SnapshotService"
codegraph callers . --symbol "ProcessNextBatch"
codegraph callees . --symbol "applyReviewRules"
codegraph impact . --symbol "CameraService"
codegraph search . "yolo inference"
codegraph stats .
```

> The Go index lags file writes by ~1 s; run `codegraph index .` after large
> restructurings.

## Testing

```bash
make test                                 # go test ./... -race -cover
go test ./internal/domain/... -v          # SQLTime, Day parsing
go test ./internal/repository/... -v      # In-memory SQLite round-trips
```

Existing tests:

- `internal/domain/models_test.go` — `SQLTime` SQLite text ↔ `time.Time` round trip,
  `Day` JSON unmarshal, `Detection` shape.
- `internal/repository/repository_test.go` — Camera / Snapshot / AnalysisJob /
  SnapshotAnalysis CRUD against an in-memory SQLite (`file::memory:?cache=shared`).

When adding tests for new repositories, use the in-memory SQLite pattern from
`openTestDB` in `repository_test.go` — it's the same engine, just without a file.

## Docker

Plug-and-play via docker compose — no host-side model export or SQLite setup needed.

```bash
# optional: configure ports/keys (skipped entirely if you use defaults)
cp .env.example .env

# default build: stub detector (pure Go) + alpine runtime with ffmpeg
docker compose up -d --build

# native gocv YOLO inference (first build downloads torch, ~1 GB; model
# is baked into the image at /models/yolov8n.onnx)
docker compose -f docker-compose.yml -f docker-compose.opencv.yml up -d --build
```

Open http://localhost:8004. `make docker-up` / `make docker-down` /
`make docker-build-opencv` are shortcuts for the same commands.

What happens on first start:

- `data/` (DB, snapshots, archives, videos, logs) is created and mounted
  at `/data`;
- `cameras.yaml` is seeded from `cameras.example.yaml` (edit the file
  inside the container or bind-mount your own — see the commented lines
  in `docker-compose.yml`), then cameras are synced idempotently on every
  start;
- `.env` is optional (`env_file` is marked `required: false`); defaults
  match the application config, `PORT` defaults to 8004.

Image variants (via `docker build --build-arg BASE=... --build-arg BUILD_TAGS=...`):

| Build | Command | Image |
| --- | --- | --- |
| Default (stub) | `docker compose build` | alpine + ffmpeg + ca-certs + tzdata, pure-Go binary. |
| OpenCV (gocv YOLO) | `make docker-build-opencv` | Adds OpenCV shared libs + baked `models/yolov8n.onnx`; bigger, first build downloads torch. |

`docker run` equivalent for the stub image:

```bash
docker run -d --name cameras-go --restart unless-stopped \
  -p 8004:8004 \
  -v $PWD/data:/data \
  --env-file .env \
  cameras-go
```

Production checklist:

- bind-mount `./data` (and `./cameras.yaml` if you keep your own camera
  list) as shown above;
- pass `.env` via `env_file` in compose or `--env-file` on `docker run`
  (or your secrets manager — never bake it into the image);
- to keep the opencv build's model up to date, rebuild with the overlay
  file (model baking is a build step, so old images keep their old model);
- put the container behind a reverse proxy (Caddy / nginx / Traefik) if
  you want TLS; the binary itself only does HTTP.

### When to move off SQLite

SQLite (pure-Go `modernc.org`, WAL mode) is the default and fits this
single-container workload up to millions of rows. If you ever need
multi-host deployment, team access, or >~5M rows, PostgreSQL is the
recommended migration target: repositories already use sqlx `Rebind()`
and `SQLTime` serializes ISO-8601, so the port is mostly
`database.go`/`schema.sql` work. MariaDB and MongoDB are not recommended
(MariaDB adds a container for no functional gain at this scale; MongoDB's
document model fights the report/join queries and needs a replica set for
transactions).

## Telegram & S3 setup

### Telegram

1. Talk to `@BotFather` on Telegram, send `/newbot`, follow the prompts.
2. Copy the bot token to `TELEGRAM_BOT_TOKEN`.
3. Send any message to your bot, then visit
   `https://api.telegram.org/bot<token>/getUpdates` to discover your chat ID (or
   add the bot to a channel and use the channel ID — note the `-100` prefix for
   channels).
4. Set `TELEGRAM_CHAT_ID` and `TELEGRAM_ENABLED=true`.

The notifier has two channels:

- **Daily timelapse** — the annotated MP4 is uploaded (or sent as a text+URL link if
  >50 MB) at the timelapse cron time.
- **Alarms** — health-check failures and scheduler panics are sent as plain text
  messages immediately.

### S3-compatible storage (Backblaze B2 example)

1. Create a bucket in B2 (or any S3 provider). Make it **public** if you want the
   share links to be directly accessible.
2. Create an application key with read/write access to that bucket.
3. Fill in `.env`:

```env
STORAGE_ENABLED=true
STORAGE_ENDPOINT_URL=https://s3.us-east-005.backblazeb2.com
STORAGE_BUCKET_NAME=your-bucket
STORAGE_ACCESS_KEY=your_key_id
STORAGE_SECRET_KEY=your_application_key
STORAGE_PUBLIC_URL=https://your-bucket.s3.us-east-005.backblazeb2.com
STORAGE_REGION=us-east-005
```

`minio-go` is used under the hood, so any S3-compatible endpoint works
(Cloudflare R2, Minio, Wasabi, etc.).

## Troubleshooting

### "database is locked" errors in logs

SQLite is a single-writer DB. The service already pauses capture/analysis during
retention, but if you see this during normal operation you can:

- increase the disk's IOPS (SQLite WAL is sensitive to slow fsync);
- lower the capture frequency (`interval_seconds` per camera);
- lower `ANALYSIS_INTERVAL_SECONDS` so jobs are drained more often.

### Camera captures all return errors

1. Test connectivity: `curl -u admin:pass http://<host>:<port>/onvif/device_service`.
2. Use `POST /api/cameras/test` to confirm the ONVIF handshake.
3. If the camera doesn't speak ONVIF, set `snapshot_url` on the camera in
   `cameras.yaml` and restart.
4. For RTSP-only cameras, ensure `ffmpeg` is on `$PATH` and the RTSP URL is reachable.

### "object detector unavailable" in logs

Two causes, both reported with a hint at startup:

- **Model file missing** — run `make model` (or `make requirements`) to download
  `models/yolov8n.onnx`, or export your own with
  `python scripts/export_yolo_onnx.py yolov8n.pt models/`.
- **Binary built without gocv** — rebuild with `make opencv` (needs OpenCV dev
  headers; `make requirements` prints the exact install command for your OS).

Without a model, the pipeline still records empty analyses — flagged items just won't
have detections to base rules on.

### Timelapse video is huge / takes forever to render

Annotated timelapses are slow by design. Knobs:

- raise `TIMELAPSE_FRAME_DURATION` (e.g. `1.0` for 1 fps);
- reduce `TIMELAPSE_OBJECT_CLASSES` to just `person`;
- raise `TIMELAPSE_WORKERS` if you have CPU headroom.

### Telegram sends text but no video

Files >50 MB hit Telegram's bot API cap. The service automatically falls back to a
text+URL link using `STORAGE_PUBLIC_URL`. Enable storage, or lower the per-frame
duration to produce a smaller file.

### Port already in use

Change `PORT` in `.env`. The Docker example uses `8004` — change the `-p` mapping
to match.

## Migrating from the Python app

The repository previously contained a Python/FastAPI implementation under `app/`. That
code has been removed; this Go port is feature-parity. If you still have a Python
install running:

- The SQLite schema is identical — you can `cp data/cameras.db data/` and the Go
  service will read it as-is.
- All env-var names match (`SNAPSHOT_RETENTION_DAYS`, `TELEGRAM_BOT_TOKEN`, …).
- `cameras.yaml` is unchanged in shape.
- The HTTP API returns the same JSON contracts. Anything calling
  `/api/cameras`, `/api/snapshots`, `/api/reviews/*`, `/api/videos/*` keeps working.
- The only operational difference: the Python service needed `ffmpeg`, Python 3.11,
  `pip install -r requirements.txt`, an APScheduler config file, and a separate
  process per concern. The Go port collapses it all to one static binary.

## Security notes

- **`.env` is gitignored** — never commit it. Rotate any secret that has been pasted
  into chat / a screenshot. The Telegram bot token and S3 access key grant real
  access.
- **HTTP basic auth** for ONVIF / snapshot URLs is passed in `cameras.yaml`. Treat
  the YAML as a secret — it's typically readable by anyone with read access to the
  project directory.
- **Path traversal** in `/snapshots/*` and `/api/videos/download/{filename}` is
  explicitly rejected (`..` segments, absolute paths).
- **CORS is wide open** by default to match the legacy config. If you expose the
  service beyond localhost, put a reverse proxy in front that tightens CORS and
  adds TLS.
- **The review API is unauthenticated.** Anyone with network access can confirm or
  reject flags. Put the service behind a reverse proxy with auth if that's a
  concern.
- **Telegram chat ID** is a numeric ID, not a `@username`. Anyone with the chat ID
  can read the channel but cannot post (only the bot can).

## Contributing

1. Fork & branch (`feat/<short-name>`, `fix/<short-name>`).
2. Keep changes small and focused. One concern per commit.
3. Follow the `AGENTS.md` rules — SOLID, conventional commits, zerolog, Google-style
   doc comments, validation in handlers.
4. Add or update tests for new behaviour. The `repository` and `domain` test patterns
   are the templates.
5. Run `make vet test fmt` before pushing. CI is not yet wired up; this is the
   contract.
6. Re-index codegraph after structural changes: `codegraph index .`.
7. Open a PR with a clear "why" (the body of a conventional commit).

Bug reports and feature requests are welcome — please include the OS, Go version,
`make test` output, and a redacted `.env` if relevant.

## License

MIT. See `LICENSE` (add one if your fork needs a different license).
