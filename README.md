# Camera Monitor

ONVIF-compatible camera snapshot monitoring system. Periodically captures snapshots from IP cameras, stores them with a web dashboard for review.

## Features

- **ONVIF auto-discovery** — connects to cameras via ONVIF protocol, fetches snapshot URIs
- **Scheduled capture** — per-camera configurable interval via APScheduler
- **Web dashboard** — view last snapshot, status, and daily reports for all cameras
- **YAML-based setup** — define cameras in `cameras.yaml`, seeded on startup
- **Docker ready** — multi-stage Alpine build, single `docker compose up`

## Quick start

```bash
# Dev server
uvicorn app.main:app --reload

# Or containerized
docker compose up --build
```

Open http://localhost:8000

## Configuration

Set via environment variables or `.env` file:

| Variable | Default | Description |
|---|---|---|
| `APP_NAME` | `Camera Monitor` | App title |
| `DEBUG` | `true` | Enable debug logging |
| `HOST` | `0.0.0.0` | Bind address |
| `PORT` | `8000` | HTTP port |
| `SNAPSHOT_RETENTION_DAYS` | `30` | Auto-delete snapshots older than this |

## Cameras YAML

Create `cameras.yaml` in the project root:

```yaml
cameras:
  - name: "Patio Trasero"
    host: "192.168.1.100"
    port: 80
    username: "admin"
    password: "cambio123"
    interval_minutes: 15
    enabled: true
```

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
| `POST` | `/api/cameras/{id}/test` | Test ONVIF connection |
| `POST` | `/api/cameras/{id}/capture` | Force snapshot |
| `GET` | `/api/snapshots/{id}` | Get snapshot |
| `GET` | `/api/snapshots/{camera_id}/by-date` | Snapshots by camera + date |
| `GET` | `/api/snapshots/image/{id}` | Snapshot JPEG file |
| `GET` | `/api/report/{date}` | Daily report data |

## Architecture

```
app/
├── main.py              # FastAPI app + lifespan (init DB, seed, scheduler)
├── core/                # config, database engine, UnitOfWork
├── domain/              # SQLAlchemy models, Pydantic schemas
├── application/         # services, repositories
├── infrastructure/      # ONVIFCameraClient (onvif-python wrapper)
├── api/                 # FastAPI routers + dependency injection
├── web/                 # Jinja2 pages + static assets
├── sql/schema.sql       # raw DDL run on startup
├── scheduler.py         # APScheduler per-camera interval jobs
└── seed.py              # YAML → DB seeder
```

Data flow: routes → services (business logic) → repositories (data access). `UnitOfWork` wraps async SQLAlchemy sessions.

## Storage

- `data/cameras.db` — SQLite database
- `data/snapshots/{camera_id}/YYYY/MM/DD/HHMM.jpg` — captured images
- `data/` is gitignored and mounted as a Docker volume
