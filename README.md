# Camera Monitor

ONVIF-compatible camera snapshot monitoring system with local ML analysis. Periodically captures snapshots from IP cameras, runs YOLO object detection, flags unusual events for human review, and provides a web dashboard for visualization.

## Features

- **ONVIF auto-discovery** — connects to cameras via ONVIF protocol, fetches snapshot URIs
- **Scheduled capture** — per-camera configurable interval via APScheduler (default 1 min)
- **Triple fallback capture** — direct URL → ONVIF `GetSnapshotUri` → RTSP+ffmpeg (auto-selects best profile)
- **ML object detection** — YOLO runs locally on every snapshot (graceful stub when `ultralytics` is not installed)
- **Review rule engine** — auto-flags persons after hours, high crowd counts, unexpected objects
- **Human review workflow** — API endpoints to list, confirm, or reject flagged snapshots
- **Web dashboard** — view last snapshot, status, daily reports, and review badges for all cameras
- **Annotated timelapse videos** — daily MP4 with YOLO detection overlays, uploaded to S3/B2 and shared via Telegram
- **Telegram notifications** — get timelapse videos and download links sent to a chat
- **S3-compatible storage** — upload large videos to Backblaze B2 or any S3-compatible provider
- **YAML-based setup** — define cameras in `cameras.yaml`, seeded on startup
- **Docker ready** — multi-stage Alpine build with ffmpeg, single `docker compose up`

## Quick start

```bash
# Dev server
uvicorn app.main:app --reload --port 8004

# Or containerized
docker compose up --build
```

Open http://localhost:8004

## Configuration

Set via environment variables or `.env` file:

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
| `TIMEZONE` | `America/Mexico_City` | Timezone for cron triggers and timestamps |
| `ANALYSIS_ENABLED` | `true` | Enable ML analysis pipeline |
| `ANALYSIS_INTERVAL_SECONDS` | `30` | How often to poll for pending analysis jobs |
| `YOLO_MODEL_PATH` | `yolov8n.pt` | Path to YOLO weights file |
| `YOLO_CONFIDENCE_THRESHOLD` | `0.5` | Minimum confidence for detection |
| `REVIEW_PERSON_AFTER_HOUR` | `22` | Hour (0-23) after which persons trigger review |
| `REVIEW_PERSON_BEFORE_HOUR` | `6` | Hour (0-23) before which persons trigger review |
| `REVIEW_MAX_PERSON_COUNT` | `5` | Max persons before auto-flagging |
| `TIMELAPSE_HOUR` | `6` | Hour when the daily annotated timelapse is generated |
| `TIMELAPSE_MINUTE` | `30` | Minute when the daily annotated timelapse is generated |
| `TIMELAPSE_CAMERA_ID` | `6` | Camera ID for the daily annotated timelapse |
| `TIMELAPSE_OBJECT_CLASSES` | `person,car,motorcycle` | Comma-separated classes to annotate |
| `TIMELAPSE_FRAME_DURATION` | `0.55` | Seconds per frame in the annotated video |
| `TIMELAPSE_WORKERS` | `3` | Parallel processes for frame annotation |
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
| `GET` | `/` | Dashboard (HTML) |
| `GET` | `/cameras` | Camera list (HTML) |
| `GET` | `/report` | Daily report (HTML) |
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

## Architecture

```
app/
├── main.py              # FastAPI app + lifespan (init DB, seed, scheduler)
├── core/                # config, database engine, UnitOfWork
├── domain/              # SQLAlchemy models, Pydantic schemas
├── application/         # services, repositories
│   ├── services/
│   │   ├── snapshot_service.py  # capture + reporting
│   │   ├── camera_service.py    # camera CRUD
│   │   ├── analysis_service.py  # ML analysis orchestrator
│   │   └── retention_service.py # archive cleanup
│   └── repositories/
│       ├── camera.py
│       ├── snapshot.py
│       ├── analysis_job.py
│       └── snapshot_analysis.py
├── infrastructure/
│   ├── onvif.py          # ONVIFCameraClient
│   ├── archive.py        # ZIP snapshot retriever
│   ├── storage.py        # StorageProvider (Backblaze B2 / S3)
│   ├── telegram.py       # TelegramNotifier (video reports)
│   └── ml/
│       ├── __init__.py
│       └── yolo.py        # YOLODetector adapter
├── api/                  # FastAPI routers + dependency injection
│   └── routers/
│       ├── cameras.py
│       ├── snapshots.py
│       ├── report.py
│       ├── videos.py
│       └── reviews.py     # review management endpoints
├── web/                  # Jinja2 pages + static assets
├── sql/schema.sql        # raw DDL run on startup
├── scheduler.py          # APScheduler (capture + analysis + retention)
└── seed.py               # YAML → DB seeder
```

Data flow: routes → services (business logic) → repositories (data access). `UnitOfWork` wraps async SQLAlchemy sessions and exposes `.cameras`, `.snapshots`, `.analysis_jobs`, and `.snapshot_analyses` repositories.

## Analysis Pipeline

After each successful snapshot capture, an `analysis_job` is enqueued. A scheduler poll (every 30s by default) picks pending jobs and runs them through:

1. **YOLO object detection** — identifies persons, vehicles, animals, and other common objects
2. **Rule engine** — applies heuristics to decide if human review is needed:
   - Person detected during restricted hours (10pm–6am)
   - Person count above threshold (default 5)
   - Unexpected object classes for the camera's view
3. **Result storage** — detection data, review flags, and anomaly scores are written to `snapshot_analyses`
4. **Review surfacing** — flagged items appear in the dashboard manifest and the review API

> The YOLO model is optional. When `ultralytics` is not installed, the detector runs in stub mode and returns empty results — the rest of the pipeline still operates without errors.

## Storage

- `data/cameras.db` — SQLite database
- `data/snapshots/{camera_id}/YYYY/MM/DD/HHMM.jpg` — captured images (raw)
- `data/videos/*.mp4` — generated timelapse videos (raw)
- `data/archives/snapshots/{camera_id}/{date}.zip` — zipped snapshots after `SNAPSHOT_ZIP_AFTER_DAYS`
- `data/archives/videos/{camera_id}/{date}.zip` — zipped videos after the same threshold
- `data/models/` — YOLO weights
- `data/logs/` — daily rotating log files (zipped after 7 days)
- `data/` is gitignored and mounted as a Docker volume

### Retention lifecycle (daily at 06:00, or via `POST /api/retention/run`)

1. **Zip** — raw files older than `SNAPSHOT_ZIP_AFTER_DAYS` (default 7) are compressed into per-camera/per-day ZIP archives under `data/archives/`; the raw file is deleted and the DB row gains an `archive_path` reference (`{zip}::{filename}`). Snapshots whose raw file is missing are marked with a `<missing>` sentinel so they aren't reprocessed.
2. **Delete** — records and orphaned archives older than `SNAPSHOT_RETENTION_DAYS` / `VIDEO_RETENTION_DAYS` (default 30) are removed. A ZIP is only deleted once all snapshots referencing it are also expired.

### Destructive purge (manual only via `fish run-purge.fish` or `POST /api/retention/purge`)

Use this when you want to permanently delete data without archiving. Default is to keep the last 3 days.

```bash
fish run-purge.fish 3   # keep last 3 days
```

This deletes raw snapshots, analysis records, analysis jobs, videos, and archives older than the given days. **No backups are created.**

### Annotated timelapse

A daily annotated MP4 is generated at `TIMELAPSE_HOUR:TIMELAPSE_MINUTE` for `TIMELAPSE_CAMERA_ID`. The video overlays bounding boxes for the configured classes (e.g. `person,car,motorcycle`). The video is saved to `data/videos/`, optionally uploaded to S3-compatible storage, and sent via Telegram.

Run manually:

```bash
fish run-timelapse.fish 5 2026-07-17
```

## Deployment

### Option 1: Docker Compose (recommended)

```bash
docker compose up --build -d
```

The compose file includes `restart: unless-stopped`, so the container auto-starts on boot and restarts on failure.

### Option 2: systemd service (bare metal)

For production deployments without Docker, use the included systemd service. This runs the app on port **8002** with automatic startup and crash recovery.

#### Prerequisites

- Fish shell installed (`sudo apt install fish` or equivalent)
- Python virtualenv at `.venv/` with dependencies installed
- `.env` file configured (copy `.env.example` and adjust)

#### Install

```bash
sudo fish manage-service.fish install
```

#### Manage

```bash
# Start the service
sudo fish manage-service.fish start

# Stop the service
sudo fish manage-service.fish stop

# Restart (after pulling new code)
sudo fish manage-service.fish restart

# Check status
sudo fish manage-service.fish status

# View live logs
sudo fish manage-service.fish logs

# Restart for development (reloads code changes, no sudo needed)
fish startup.fish --restart

# Uninstall (stop and remove the service)
sudo fish manage-service.fish uninstall

# Reinstall (fresh install)
sudo fish manage-service.fish reinstall
```

### Scripts

#### `manage-service.fish`

All-in-one service manager. Must be run with `sudo` (except `logs` which can run without).

```bash
sudo fish manage-service.fish install     # Install and start
sudo fish manage-service.fish start       # Start
sudo fish manage-service.fish stop        # Stop
sudo fish manage-service.fish restart     # Restart
sudo fish manage-service.fish status      # Show status
sudo fish manage-service.fish logs        # Follow live logs
sudo fish manage-service.fish uninstall   # Remove service
sudo fish manage-service.fish reinstall   # Fresh reinstall
```

| Script | Purpose | Port | Auto-restart |
|---|---|---|---|---|
| `manage-service.fish` | Full service manager — install, start, stop, restart, status, logs, uninstall | — | — |
| `restart.fish` | Manual restart — kills existing process, starts fresh in background | 8002 | No (background via nohup) |
| `startup.fish` | Systemd entrypoint with `--restart` for dev — prepares env, downloads model, execs uvicorn | 8002 | Yes (via `Restart=always`) |
| `run-retention.fish` | Run retention cleanup manually — zips old files, deletes expired | — | — |
| `run-purge.fish` | Destructively purge snapshots/analyses/videos older than N days | — | — |
| `run-timelapse.fish` | Generate annotated timelapse on demand for a camera/date | — | — |

#### `restart.fish`

Use for manual restarts during development or when you need to kill and re-launch the server from a terminal.

```bash
fish restart.fish
```

- Kills any running `uvicorn app.main:app` process
- Waits up to 5 seconds for port 8002 to free
- Downloads YOLO model if missing
- Generates dashboard manifest
- Starts uvicorn in the background via `nohup`
- Logs written to `/tmp/uvicorn.log`

#### `startup.fish`

Primary entrypoint for both systemd and development. Uses `exec` to replace the shell process with uvicorn, allowing systemd to track the process lifecycle.

```bash
# Normal start (systemd uses this)
fish startup.fish

# Restart for development (kills existing process, waits for port to free)
fish startup.fish --restart
```

Modes:
- **No flags** — clean start, assumes no existing process (used by systemd)
- **`--restart`** — kills existing uvicorn, waits up to 5 seconds for port 8002 to free, then starts

Key differences from `restart.fish`:
- Uses `exec` instead of `nohup` (foreground process)
- Resolves paths via `realpath` instead of `$PWD` (works from any working directory)
- No background logging to `/tmp/uvicorn.log` (output goes to terminal or journal)

#### `camera-monitor.service`

Systemd unit file for production deployment. Features:

- **Auto-start on boot** — `WantedBy=multi-user.target`
- **Crash recovery** — `Restart=always` with 5-second delay
- **Environment** — loads `.env` file, sets `PATH` to include virtualenv
- **Logging** — stdout/stderr routed to systemd journal
- **Network dependency** — waits for `network-online.target` before starting

#### `run-retention.fish` (mandatory for manual runs)

Run the retention/archive cleanup job manually. **Always use this script** instead of calling `POST /api/retention/run` directly — it pauses capture and analysis schedulers to avoid SQLite write contention, has a 30-minute timeout, and pretty-prints the result.

```bash
fish run-retention.fish
```

Equivalent to the daily 06:00 AM cron. Zips snapshots/videos older than `SNAPSHOT_ZIP_AFTER_DAYS` and deletes records past `SNAPSHOT_RETENTION_DAYS`.

#### `run-purge.fish` (destructive)

Permanently delete snapshots, analyses, videos, and archives older than N days **without creating ZIP archives**. Default is 3 days.

```bash
fish run-purge.fish      # keep last 3 days
fish run-purge.fish 7    # keep last 7 days
```

The script pauses capture/analysis jobs, calls `POST /api/retention/purge`, and pretty-prints the result. There is a 3-second warning delay before the request is sent.

#### `run-timelapse.fish`

Generate an annotated timelapse on demand for a camera and date.

```bash
fish run-timelapse.fish                         # camera 6, yesterday, default classes
fish run-timelapse.fish 5 2026-07-17            # camera 5, specific date
fish run-timelapse.fish 5 2026-07-17 "person"   # custom classes
```
