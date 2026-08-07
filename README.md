# Camera Monitor (Go)

ONVIF-compatible camera snapshot monitoring system with local ML analysis. Periodically captures snapshots from IP cameras, runs YOLO object detection, flags unusual events for human review, and provides a web dashboard for visualization. Single Go binary, feature-parity port of the original Python/FastAPI implementation.

## Features

- **ONVIF client** — connects to cameras via ONVIF protocol, fetches snapshot URIs
- **Scheduled capture** — per-camera configurable interval (default 1 min), restart-anchored timers
- **Triple fallback capture** — direct URL → ONVIF `GetSnapshotUri` → RTSP+ffmpeg (auto-selects best profile)
- **ML object detection** — YOLO runs locally on every snapshot (graceful stub mode when the model is missing)
- **Review rule engine** — auto-flags persons after hours, high crowd counts, unexpected objects
- **Human review workflow** — API endpoints to list, confirm, or reject flagged snapshots
- **Web dashboard** — view last snapshot, status, daily reports, and review badges for all cameras
- **Annotated timelapse videos** — daily MP4 with YOLO detection overlays, uploaded to S3/B2 and shared via Telegram
- **Telegram notifications** — get timelapse videos and download links sent to a chat
- **S3-compatible storage** — upload large videos to Backblaze B2 or any S3-compatible provider
- **YAML-based setup** — define cameras in `cameras.yaml`, seeded on startup
- **Docker ready** — multi-stage build (`scratch` default, `opencv` base adds ffmpeg)

## Quick start

```bash
make run        # dev server on :8004, data dir ./data
make build      # compile bin/cameras-go
make test       # go test ./... -race -cover
make vet        # go vet ./...
make opencv     # build with native YOLO via gocv (-tags opencv)
make docker     # docker build -t cameras-go .
```

Open http://localhost:8004

## Configuration

Set via environment variables or `.env` file at the working directory:

| Variable | Default | Description |
|---|---|---|
| `APP_NAME` | `Camera Monitor` | App title |
| `DEBUG` | `true` | Enable debug logging |
| `HOST` | `0.0.0.0` | Bind address |
| `PORT` | `8000` | HTTP port |
| `SNAPSHOT_RETENTION_DAYS` | `30` | Auto-delete snapshot records/archives older than this |
| `SNAPSHOT_ZIP_AFTER_DAYS` | `7` | Zip raw snapshots older than this into daily archives |
| `VIDEO_RETENTION_DAYS` | `30` | Auto-delete video archives older than this |
| `DEFAULT_INTERVAL_SECONDS` | `60` | Default capture interval for new cameras |
| `CAPTURE_TIMEOUT_SECONDS` | `30` | Timeout for a single capture attempt |
| `HEALTH_CHECK_INTERVAL_MINUTES` | `10` | How often to health-check cameras |
| `TIMEZONE` | `America/Mexico_City` | Timezone for cron triggers and timestamps |
| `ANALYSIS_ENABLED` | `true` | Enable ML analysis pipeline |
| `ANALYSIS_INTERVAL_SECONDS` | `30` | How often to poll for pending analysis jobs |
| `YOLO_MODEL_PATH` | `yolov8n.onnx` | Path to YOLO ONNX weights file |
| `YOLO_CONFIDENCE_THRESHOLD` | `0.5` | Minimum confidence for detection |
| `REVIEW_PERSON_AFTER_HOUR` | `22` | Hour (0-23) after which persons trigger review |
| `REVIEW_PERSON_BEFORE_HOUR` | `6` | Hour (0-23) before which persons trigger review |
| `REVIEW_MAX_PERSON_COUNT` | `5` | Max persons before auto-flagging |
| `TIMELAPSE_HOUR` | `6` | Hour when the daily annotated timelapse is generated |
| `TIMELAPSE_MINUTE` | `30` | Minute when the daily annotated timelapse is generated |
| `TIMELAPSE_CAMERA_ID` | `6` | Camera ID for the daily annotated timelapse |
| `TIMELAPSE_OBJECT_CLASSES` | `person,car,motorcycle` | Comma-separated classes to annotate |
| `TIMELAPSE_FRAME_DURATION` | `0.55` | Seconds per frame in the annotated video |
| `TIMELAPSE_WORKERS` | `3` | Parallel workers for frame annotation |
| `TELEGRAM_ENABLED` | `false` | Send Telegram notifications |
| `TELEGRAM_BOT_TOKEN` | `""` | Telegram bot token |
| `TELEGRAM_CHAT_ID` | `""` | Telegram chat ID |
| `STORAGE_ENABLED` | `false` | Upload videos to S3-compatible storage |
| `STORAGE_ENDPOINT_URL` | `""` | S3 endpoint (e.g. Backblaze B2) |
| `STORAGE_BUCKET_NAME` | `""` | Bucket name |
| `STORAGE_ACCESS_KEY` | `""` | Access key ID |
| `STORAGE_SECRET_KEY` | `""` | Secret access key |
| `STORAGE_PUBLIC_URL` | `""` | Public base URL for uploaded files |
| `STORAGE_REGION` | `us-west-004` | S3 region |

## Cameras YAML

Create `cameras.yaml` in the project root:

```yaml
cameras:
  - name: "Patio Trasero"
    host: "192.168.1.100"
    port: 80
    username: "admin"
    password: "cambio123"
    interval_seconds: 60
    enabled: true
    snapshot_url: "http://192.168.1.100/cgi-bin/snapshot.cgi"  # optional override
```

## Snapshot capture fallback

Each camera attempt uses the following strategy (first success wins):

1. **Direct URL** — if `snapshot_url` is set on the camera, HTTP GET that URL with HTTP Basic Auth
2. **ONVIF `GetSnapshotUri`** — standard ONVIF snapshot pull (most compatible)
3. **RTSP+ffmpeg** — ONVIF `GetStreamUri` → ffmpeg frame grab (auto-selects highest-resolution profile)

This means cameras that don't support `GetSnapshotUri` (e.g. cheap NVRs, older models) still work via RTSP.

## API

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Dashboard (HTML, embedded SPA) |
| `GET` | `/reviews` | Review workflow (HTML) |
| `GET` | `/api/cameras` | List cameras |
| `POST` | `/api/cameras` | Add camera |
| `GET` | `/api/cameras/{id}` | Camera detail |
| `PUT` | `/api/cameras/{id}` | Update camera |
| `DELETE` | `/api/cameras/{id}` | Remove camera |
| `POST` | `/api/cameras/test` | Test ONVIF connection |
| `POST` | `/api/cameras/{id}/snapshot` | Force snapshot |
| `GET` | `/api/snapshots/{id}` | Get snapshot metadata (includes analysis) |
| `GET` | `/api/snapshots/{camera_id}/by-date` | Snapshots by camera + date |
| `GET` | `/api/snapshots/image/{id}` | Snapshot JPEG file |
| `GET` | `/api/report/{date}` | Daily report data (includes analysis) |
| `POST` | `/api/videos/generate` | Generate a timelapse video for a camera/date |
| `GET` | `/api/videos/{filename}` | Download a generated or archived video |
| `GET` | `/api/reviews/pending` | List snapshots flagged for review |
| `GET` | `/api/reviews/count` | Count of pending reviews |
| `GET` | `/api/reviews/detections` | Paginated detections browser (supports `days_back`, `date_from`, `camera_id`, `class_name`, `limit`, `offset`) |
| `POST` | `/api/reviews/{id}/review` | Confirm or reject a review flag |
| `POST` | `/api/retention/run` | Trigger the retention/archive cleanup job on demand |
| `POST` | `/api/retention/purge` | Destructively purge snapshots/analyses/videos older than N days |
| `POST` | `/api/videos/annotated` | Generate an annotated timelapse with YOLO detection overlays |
| `GET` | `/healthz` | Health check |

## Architecture

```
cmd/server/main.go            # wiring: config → DB → services → scheduler → HTTP
internal/
├── config/config.go          # caarlos0/env + .env loader
├── database/database.go      # sqlx + pure-Go SQLite (WAL PRAGMAs), //go:embed schema.sql, legacy migrations
├── domain/
│   ├── models.go             # entities + SQLTime (SQLite text ↔ time, isoformat JSON)
│   └── schemas.go            # request/response DTOs, Day (accepts "YYYY-MM-DD")
├── repository/               # 4 repos over DBTX (*sqlx.DB or *sqlx.Tx)
├── service/                  # camera, snapshot, analysis, retention, timelapse
├── infrastructure/
│   ├── onvif/onvif.go        # use-go/onvif adapter (namespace-agnostic XML parsing)
│   ├── ml/                   # Detector interface; stub.go default; engine_opencv.go behind -tags opencv
│   ├── telegram/notifier.go  # 50 MB cap + S3 fallback
│   ├── storage/s3.go         # minio-go
│   └── archive/archive.go    # ZIP reference "zip::filename" reader
├── api/                      # chi router + handlers (validation lives here)
├── scheduler/                # per-camera timers (restart-anchored) + robfig/cron jobs
├── seed/seed.go              # YAML → DB idempotent sync
└── web/static/               # embedded SPA (index.html, reviews.html, app.js, app.css)
```

Data flow: handlers → services (business logic) → repositories (data access). No UnitOfWork: services open `*sqlx.Tx` and pass it to repos. All timestamps are stored as SQLite text (`YYYY-MM-DD HH:MM:SS`) in the project timezone and emitted as ISO-8601 in JSON.

## Analysis Pipeline

After each successful snapshot capture, an `analysis_job` is enqueued. A scheduler poll (every 30s by default) picks pending jobs and runs them through:

1. **YOLO object detection** — identifies persons, vehicles, animals, and other common objects
2. **Rule engine** — applies heuristics to decide if human review is needed:
   - Person detected during restricted hours (10pm–6am)
   - Person count above threshold (default 5)
   - Unexpected object classes for the camera's view
3. **Result storage** — detection data, review flags, and anomaly scores are written to `snapshot_analyses`
4. **Review surfacing** — flagged items appear in the dashboard manifest and the review API

> The YOLO model is optional. The default build ships with a stub detector (no OpenCV dependency) and returns empty results; build with `-tags opencv` for native inference over an ONNX export (see `scripts/export_yolo_onnx.py` to produce it from a `.pt` checkpoint). The rest of the pipeline operates without errors in either mode.

## Storage

- `data/cameras.db` — SQLite database
- `data/snapshots/{camera_id}/YYYY/MM/DD/HHMM.jpg` — captured images (raw)
- `data/videos/*.mp4` — generated timelapse videos (raw)
- `data/archives/snapshots/{camera_id}/{date}.zip` — zipped snapshots after `SNAPSHOT_ZIP_AFTER_DAYS`
- `data/archives/videos/{camera_id}/{date}.zip` — zipped videos after the same threshold
- `data/models/` — YOLO ONNX weights
- `data/logs/` — daily rotating log files
- `data/` is gitignored and mounted as a Docker volume

### Retention lifecycle (daily at 06:00, or via `POST /api/retention/run`)

1. **Zip** — raw files older than `SNAPSHOT_ZIP_AFTER_DAYS` (default 7) are compressed into per-camera/per-day ZIP archives under `data/archives/`; the raw file is deleted and the DB row gains an `archive_path` reference (`{zip}::{filename}`). Snapshots whose raw file is missing are marked with a `<missing>` sentinel so they aren't reprocessed.
2. **Delete** — records and orphaned archives older than `SNAPSHOT_RETENTION_DAYS` / `VIDEO_RETENTION_DAYS` (default 30) are removed. A ZIP is only deleted once all snapshots referencing it are also expired.

Retention pauses capture/analysis jobs while running to avoid SQLite write contention.

### Destructive purge (manual only via `POST /api/retention/purge`)

Use this when you want to permanently delete data without archiving. Default is to keep the last 3 days.

```bash
curl -X POST -H "Content-Type: application/json" \
  -d '{"days": 3}' http://localhost:8004/api/retention/purge
```

This deletes raw snapshots, analysis records, analysis jobs, videos, and archives older than the given days. **No backups are created.**

### Annotated timelapse

A daily annotated MP4 is generated at `TIMELAPSE_HOUR:TIMELAPSE_MINUTE` for `TIMELAPSE_CAMERA_ID`. The video overlays bounding boxes for the configured classes (e.g. `person,car,motorcycle`). The video is saved to `data/videos/`, optionally uploaded to S3-compatible storage, and sent via Telegram.

Run manually:

```bash
curl -X POST -H "Content-Type: application/json" \
  -d '{"camera_id": 5, "date": "2026-07-17"}' http://localhost:8004/api/videos/annotated
```

## Docker

```bash
make docker                                # scratch image (stub detector)
docker build --build-arg BASE=opencv -t cameras-go .  # alpine + ffmpeg + OpenCV

docker run -d --name cameras-go --restart unless-stopped \
  -p 8004:8000 \
  -v $PWD/data:/data \
  -v $PWD/cameras.yaml:/cameras.yaml \
  --env-file .env \
  cameras-go
```

## Development

```bash
make vet        # go vet ./...
make test       # go test ./... -race -cover
make opencv     # build with native YOLO via gocv (requires OpenCV dev headers)
```

The repository is indexed by [codegraph](https://github.com/isink17/codegraph) — run `codegraph index .` after restructuring packages.
