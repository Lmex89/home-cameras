# Cameras — ONVIF snapshot monitor

## Quick start

```bash
uvicorn app.main:app --reload          # dev server
docker compose up --build              # containerized
```

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

DDD-lite: routes → services (contain logic) → repos (data access). `UnitOfWork` wraps an async SQLAlchemy session and exposes `.cameras` and `.snapshots` repos.

## Key flows

| Step | What happens |
|---|---|
| Startup | `lifespan` → init DB from `sql/schema.sql` → seed from `cameras.yaml` → start APScheduler |
| Snapshots | `scheduler` calls `capture_job` → `ONVIFCameraClient.get_snapshot_uri()` → `httpx` fetches JPEG → saved to `data/snapshots/{camera_id}/Y/m/d/HM.jpg` |
| Data dirs | `data/` is gitignored, mounted as Docker volume. Contains `cameras.db` and `snapshots/`. |

## Config

Env vars (via `pydantic-settings`, reads `.env`):

- `APP_NAME`, `DEBUG`, `HOST`, `PORT`, `SNAPSHOT_RETENTION_DAYS`

## Docker

Multi-stage Alpine build. `docker-compose.yml` mounts `data/` and `cameras.yaml`. App user is `appuser` (UID not fixed).

## Dependencies

- FastAPI, uvicorn, SQLAlchemy (async), aiosqlite, Jinja2
- onvif-python, httpx (for snapshot fetch)
- APScheduler (async), PyYAML, pydantic-settings

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
