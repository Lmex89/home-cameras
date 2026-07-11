# Cameras — ONVIF snapshot monitor

## Quick start

```bash
uvicorn app.main:app --reload --port 8004  # dev server
docker compose up --build                  # containerized
```

## Service management

For production deployments on bare metal, use the systemd service scripts:

```bash
# Install and enable auto-start on boot
sudo fish manage-service.fish install

# Start / stop / restart
sudo fish manage-service.fish start
sudo fish manage-service.fish stop
sudo fish manage-service.fish restart

# Check status and logs
sudo fish manage-service.fish status
sudo fish manage-service.fish logs

# Development restart (no sudo, reloads code changes)
fish startup.fish --restart

# Uninstall
sudo fish manage-service.fish uninstall
```

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

DDD-lite: routes → services (contain logic) → repos (data access). `UnitOfWork` wraps an async SQLAlchemy session and exposes `.cameras`, `.snapshots`, `.analysis_jobs`, and `.snapshot_analyses` repos.

## Key flows

| Step | What happens |
|---|---|
| Startup | `lifespan` → init DB from `sql/schema.sql` → seed from `cameras.yaml` → start APScheduler |
| Snapshots | `scheduler` calls `capture_job` → `SnapshotService.capture()` tries: direct URL → ONVIF `GetSnapshotUri` → RTSP+ffmpeg → saved to `data/snapshots/{camera_id}/Y/m/d/HM.jpg` |
| Analysis  | After each successful capture, `AnalysisService.analyze_snapshot()` enqueues an `analysis_job`. A separate scheduler poll (every 30s) processes pending jobs via `process_next_batch()`. |
| Review    | `AnalysisService._apply_review_rules()` flags snapshots for human review (person after hours, high count, unexpected objects). Review items surface in the manifest and `/api/reviews/pending` endpoint. |
| Retention | Daily 03:00 cron (`schedule_retention`) → `RetentionService.run()`: zips raw files older than `SNAPSHOT_ZIP_AFTER_DAYS` into `data/archives/`, then deletes records/archives past `SNAPSHOT_RETENTION_DAYS` / `VIDEO_RETENTION_DAYS`. Also triggerable on demand via `POST /api/retention/run`. |
| Data dirs | `data/` is gitignored, mounted as Docker volume. Contains `cameras.db`, `snapshots/` (raw), `videos/` (raw), `archives/` (zipped), `models/`, and `logs/`. |

## Config

Env vars (via `pydantic-settings`, reads `.env`):

- `APP_NAME`, `DEBUG`, `HOST`, `PORT`, `TIMEZONE`, `SNAPSHOT_RETENTION_DAYS`, `SNAPSHOT_ZIP_AFTER_DAYS`, `VIDEO_RETENTION_DAYS`, `DEFAULT_INTERVAL_SECONDS`
- `ANALYSIS_ENABLED`, `ANALYSIS_INTERVAL_SECONDS`, `YOLO_MODEL_PATH`, `YOLO_CONFIDENCE_THRESHOLD`
- `REVIEW_PERSON_AFTER_HOUR`, `REVIEW_PERSON_BEFORE_HOUR`, `REVIEW_MAX_PERSON_COUNT`

## Docker

Multi-stage Alpine build. Includes `ffmpeg` for RTSP snapshot fallback. `docker-compose.yml` mounts `data/` and `cameras.yaml`. App user is `appuser` (UID not fixed).

For GPU-accelerated ML inference, a separate worker image based on `nvidia/cuda` is planned.

## Dependencies

- FastAPI, uvicorn, SQLAlchemy (async), aiosqlite, Jinja2
- onvif-python, httpx (for snapshot fetch)
- APScheduler (async), PyYAML, pydantic-settings
- ffmpeg (RTSP frame grab fallback)
- ultralytics (YOLO inference, graceful stub mode when missing)

## Mandatory: SOLID principles

Every contribution MUST follow SOLID:

- **Single Responsibility**: One class/service = one reason to change. `CameraService` handles camera CRUD, `SnapshotService` handles capture/reporting, `ONVIFCameraClient` handles ONVIF wire protocol. Do not blur concerns.
- **Open/Closed**: Extend via new classes, not modification of existing stable ones. New camera vendor? Add a new infrastructure adapter, do not touch existing services.
- **Liskov Substitution**: Subtypes must be replaceable for their base. Keep repository interfaces consistent — all repos follow the same `add/get_by_id/get_all` pattern.
- **Interface Segregation**: Keep abstractions narrow. `CameraRepository` exposes only what callers need, not kitchen-sink interfaces.
- **Dependency Inversion**: Depend on abstractions (protocols / ABCs / type stubs), not concretions. Services receive `UnitOfWork` and `ONVIFCameraClient` via constructor injection.

## Mandatory: Conventional commits

Every commit MUST follow `conventionalcommits.org` v1.0.0:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

Allowed types: `feat`, `fix`, `build`, `chore`, `ci`, `docs`, `style`, `refactor`, `perf`, `test`, `revert`.
Breaking changes: append `!` after type/scope OR add `BREAKING CHANGE:` footer.
Body explains *why* (not what). Footer references issues or breaking changes.

## Mandatory: Loguru logging

Every log MUST use `loguru` with the correct severity level. The project uses `from loguru import logger` — no `print()`, no `logging` stdlib.

| Level | When to use |
|---|---|
| `logger.debug(...)` | Development-only details: ONVIF request/response payloads, raw URIs, profile tokens |
| `logger.info(...)` | Routine operations: snapshot saved, camera scheduled, seed completed |
| `logger.warning(...)` | Recoverable issues: camera unreachable on one attempt, YAML parse fallback, deprecated config |
| `logger.error(...)` | Operation failures: snapshot capture failed, DB init error, ONVIF connection timeout |
| `logger.exception(...)` | Inside `except` blocks only — logs the full traceback automatically. Never use `logger.error` in an except handler if you want the traceback. |

Use f-string formatting (not `{}` positional args): `logger.info(f"Camera {cam_id} snapshot saved")`.

## Mandatory: Pydantic validation & serialization

Every endpoint MUST use pydantic schemas for both input validation and output serialization:

- **Input**: Use `StringConstraints` (`min_length`, `strip_whitespace`) and `Field` bounds (`ge`, `le`) on create/update schemas. Never trust raw request params without type constraints.
- **Output**: Always call `Schema.model_validate(orm_obj)` before returning ORM data. Never return raw ORM instances — FastAPI `response_model` alone is not sufficient for explicit validation.
- **Serialization**: Use `.model_dump()` / `.model_dump(exclude_unset=True)` when converting schemas to dicts for service layers.
- **Config**: Always use `ConfigDict(from_attributes=True)` on read schemas, never bare dicts.

## Mandatory: Google-style docstrings

Every module, class, function, and method MUST have a Google-style docstring. FastAPI path operations use the docstring as the OpenAPI description (supports Markdown).

### Format

```
"""Single-line summary (max 80 chars, ends with period).

Optionally leave a blank line, then longer description. Sections are
separated by blank lines.
```

### Sections by context

| Context | Required sections |
|---|---|
| **Module** | Summary describing purpose and contents |
| **Class** | Summary + description of responsibility |
| **Function / Method** | Summary, `Args:` (if any params), `Returns:` (if not None), `Raises:` (if any) |
| **FastAPI route** | Summary + Markdown body (used as OpenAPI `description`). Use `\f` to truncate for OpenAPI if the docstring is long. |
| **Property** | Summary only (unless complex) |
| **`__init__`** | Document in the class docstring instead, or use `Args:` on `__init__` |

### Rules

1. **Always `"""` triple double-quotes** — never `'''` or `#` comments for docstrings.
2. **Summary on first line** — imperative mood ("Get the user", not "Gets the user").
3. **`Args:`** — one line per parameter: `param_name: Description.` Include type only if non-obvious.
4. **`Returns:`** — describe the return value and its type: `The snapshot file path.` Omit if `-> None`.
5. **`Raises:`** — one line per exception: `ValueError: Description of when it occurs.`
6. **Types in docstrings** — do NOT duplicate type annotations. The type hints serve that purpose. Docstrings describe *meaning and behavior*.
7. **No docstring is worse than a bad one** — if a function is trivial (`@property` returning `self._x`), a one-line summary is acceptable.

### Examples

```python
async def get_daily_report(self, target_date: date) -> dict:
    """Group all snapshots for a date by camera.

    Args:
        target_date: The date to query (in project timezone).

    Returns:
        Dict keyed by camera_id with camera name and snapshot list,
        or empty dict if no snapshots exist.
    """
    ...
```

```python
@app.post("/items/")
async def create_item(payload: ItemCreate) -> ItemRead:
    """Create a new item.

    Validates the input and persists to the database. Returns the
    created item with generated fields populated.

    \f
    **Notes:**
    - Name must be unique within the project.
    - Tags are lowercased on creation.
    """
    ...
```
