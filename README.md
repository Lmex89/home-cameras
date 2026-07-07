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

### Retention lifecycle (daily at 03:00, or via `POST /api/retention/run`)

1. **Zip** — raw files older than `SNAPSHOT_ZIP_AFTER_DAYS` (default 7) are compressed into per-camera/per-day ZIP archives under `data/archives/`; the raw file is deleted and the DB row gains an `archive_path` reference (`{zip}::{filename}`). Snapshots whose raw file is missing are marked with a `<missing>` sentinel so they aren't reprocessed.
2. **Delete** — records and orphaned archives older than `SNAPSHOT_RETENTION_DAYS` / `VIDEO_RETENTION_DAYS` (default 30) are removed. A ZIP is only deleted once all snapshots referencing it are also expired.
