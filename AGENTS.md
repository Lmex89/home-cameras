# Cameras — ONVIF snapshot monitor

Dual-implementation repo: the legacy **Python/FastAPI** app lives in `app/`,
and the **Go port** (feature-parity, same DB schema, single binary) lives in
`cmd/` + `internal/`. See `GO_MIGRATION.md` for the file-by-file mapping and
Python→Go cheat sheet. When a feature or bug touches business logic, apply the
change to **both** implementations unless told otherwise.

## 🔴 ABSOLUTE: Codegraph-only lookup

You MUST use codegraph_* tools (codegraph_find_symbol, codegraph_context_for_task, etc.) for ALL code search and navigation.

**NEVER use `glob`, `grep`, or `read` for code lookup.** Codegraph tools are the ONLY permitted approach. Built-in tools are ONLY permitted when a codegraph tool returns no useful results, as a strictly last resort.

**Re-index after Go changes:** the Go index lags writes; run `codegraph index .` after restructuring packages.

## Quick start

```bash
# ── Go port (preferred) ─────────────────────────────────────────────
make run        # dev server on :8004, data dir ./data
make build      # compile bin/cameras-go
make test       # go test ./... -race -cover
make vet        # go vet ./...
make opencv     # build with native YOLO via gocv (-tags opencv)
make docker     # docker build -t cameras-go .

# ── Legacy Python app ───────────────────────────────────────────────
uvicorn app.main:app --reload --port 8004
docker compose up --build
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

# Run retention cleanup manually (MANDATORY command)
# Always use this script for manual retention; it pauses capture/analysis
# jobs to avoid SQLite lock contention and reports the result correctly.
fish run-retention.fish

# Purge snapshots/analyses/videos older than N days (DESTRUCTIVE)
# Default keeps last 3 days. Use with caution — no archives are created.
fish run-purge.fish

# Uninstall
sudo fish manage-service.fish uninstall
```

## Architecture

### Go port (`cmd/` + `internal/`) — the current implementation

```
cmd/server/main.go            # wiring: config → DB → services → scheduler → HTTP
internal/
├── config/config.go          # caarlos0/env + .env loader (env names unchanged)
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

DDD-lite, same as Python: handlers → services (contain logic) → repositories
(data access). No UnitOfWork: services open `*sqlx.Tx` and pass it to repos.
Docs use Google-style sections (`Args:`, `Returns:`, `Raises:`) so Python
developers can migrate between the two trees.

### Legacy Python app (`app/`) — frozen

```
app/
├── main.py              # FastAPI app + lifespan (init DB, seed, scheduler)
├── core/                # config, database engine, UnitOfWork
├── domain/              # SQLAlchemy models, Pydantic schemas
├── application/         # services, repositories
├── infrastructure/      # onvif, archive, storage, telegram, ml/yolo
├── api/routers/         # cameras, snapshots, report, videos, reviews
├── web/                 # Jinja2 pages + static assets
├── sql/schema.sql       # raw DDL run on startup
├── scheduler.py         # APScheduler
└── seed.py              # YAML → DB seeder
```

## Key flows (both implementations)

| Step | What happens |
|---|---|
| Startup | init DB from `sql/schema.sql` + legacy migrations → seed from `cameras.yaml` → start scheduler (Go: `scheduler.New` + `sched.Start()`) |
| Snapshots | Per-camera interval job → capture tries: direct URL → ONVIF `GetSnapshotUri` → RTSP+ffmpeg → saved to `data/snapshots/{camera_id}/Y/m/d/HMSS.jpg` |
| Analysis  | After each successful capture an `analysis_job` is enqueued; a poller (every 30s) processes pending jobs (Go: `AnalysisService.ProcessNextBatch`). Detector is a singleton; stub mode when the model is missing. |
| Review    | `applyReviewRules` flags snapshots (person after hours, high count, unexpected objects). Surfaces in `/data/manifest.json` and `/api/reviews/pending`. |
| Retention | Daily 06:00 cron → zips raw files older than `SNAPSHOT_ZIP_AFTER_DAYS` into `data/archives/`, deletes records/archives past `SNAPSHOT_RETENTION_DAYS` / `VIDEO_RETENTION_DAYS`. Pauses capture/analysis jobs while running. Also `POST /api/retention/run`. |
| Purge     | Destructive `POST /api/retention/purge` or `fish run-purge.fish`: deletes snapshots, analyses, jobs, videos, archives older than `days` **without** creating archives. |
| Timelapse | Daily cron at `TIMELAPSE_HOUR` generates the annotated MP4 (ffmpeg concat + faststart); uploaded to S3-compatible storage and sent via Telegram. |
| Telegram  | After a timelapse is saved, the MP4 is sent to the configured chat; text+URL fallback above 50 MB. Alarms on scheduled-job panic/failure. |
| Data dirs | `data/` is gitignored, mounted as Docker volume: `cameras.db`, `snapshots/`, `videos/`, `archives/`, `models/`, `logs/`. |

## Config

Env vars are identical for both implementations (`.env` at the working
directory; Go: `config.Load()` via caarlos0/env, Python: `pydantic-settings`):

- `APP_NAME`, `DEBUG`, `HOST`, `PORT`, `TIMEZONE`, `SNAPSHOT_RETENTION_DAYS`, `SNAPSHOT_ZIP_AFTER_DAYS`, `VIDEO_RETENTION_DAYS`, `DEFAULT_INTERVAL_SECONDS`, `CAPTURE_TIMEOUT_SECONDS`, `HEALTH_CHECK_INTERVAL_MINUTES`
- `ANALYSIS_ENABLED`, `ANALYSIS_INTERVAL_SECONDS`, `YOLO_MODEL_PATH`, `YOLO_CONFIDENCE_THRESHOLD`
- `REVIEW_PERSON_AFTER_HOUR`, `REVIEW_PERSON_BEFORE_HOUR`, `REVIEW_MAX_PERSON_COUNT`
- `TELEGRAM_ENABLED`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`
- `TIMELAPSE_HOUR`, `TIMELAPSE_MINUTE`, `TIMELAPSE_CAMERA_ID`, `TIMELAPSE_OBJECT_CLASSES`, `TIMELAPSE_FRAME_DURATION`, `TIMELAPSE_WORKERS`
- `STORAGE_ENABLED`, `STORAGE_ENDPOINT_URL`, `STORAGE_BUCKET_NAME`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY`, `STORAGE_PUBLIC_URL`, `STORAGE_REGION`

## Docker

- **Python**: multi-stage Alpine build, includes `ffmpeg`; `docker-compose.yml` mounts `data/` and `cameras.yaml`.
- **Go**: `Dockerfile` multi-stage (`FROM scratch` default, `--build-arg BASE=opencv` for an alpine image with ffmpeg).

## Dependencies

- **Go**: chi/v5, sqlx, modernc.org/sqlite (pure Go, no CGO), caarlos0/env, zerolog, robfig/cron/v3, use-go/onvif, go-telegram-bot-api (via raw HTTP in the notifier), minio-go, fogleman/gg + golang.org/x/image (frame annotation), gopkg.in/yaml.v3, gocv (optional, `-tags opencv`)
- **Python**: FastAPI, uvicorn, SQLAlchemy (async), aiosqlite, Jinja2, onvif-python, httpx, APScheduler, PyYAML, pydantic-settings, python-telegram-bot, boto3, ultralytics (stub mode when missing)
- Both: ffmpeg (RTSP frame grab + timelapse assembly)

## Codegraph (code context engine)

A local-first code context engine that builds a persistent knowledge graph in SQLite
for AI coding assistants. Provides symbol lookup, call graph traversal, impact
analysis, and semantic search — zero cloud dependencies.

### Setup

```bash
# Install (requires Go 1.23+ and a C compiler)
go install github.com/isink17/codegraph/cmd/codegraph@latest

# Re-index after code changes
export PATH="$PATH:$(go env GOPATH)/bin"
codegraph index .
```

### Usage

The MCP server is configured in `.mcp.json` and starts automatically with OpenCode.
Manual CLI queries:

```bash
codegraph find-symbol . "<query>"     # Find symbols by name
codegraph callers . --symbol "<name>" # Find callers of a function
codegraph callees . --symbol "<name>" # Find callees of a function
codegraph impact . --symbol "<name>"  # Impact analysis
codegraph search . "<query>"          # Full-text symbol search
codegraph stats .                     # Graph statistics
```

## Mandatory: SOLID principles

Every contribution MUST follow SOLID:

- **Single Responsibility**: One type/package = one reason to change. `CameraService` handles camera CRUD, `SnapshotService` handles capture/reporting, `onvif.Client` handles the wire protocol. Do not blur concerns.
- **Open/Closed**: Extend via new types, not modification of existing stable ones. New camera vendor? Add a new infrastructure adapter. New ML backend? New `ml.Detector` implementation.
- **Liskov Substitution**: Subtypes must be replaceable for their base. Keep repository signatures consistent — all repos follow the same `Add/GetByID/GetAll` pattern over `DBTX`.
- **Interface Segregation**: Keep abstractions narrow. `DBTX` exposes only what repositories need; `ml.Detector` only `Available()` + `Detect()`.
- **Dependency Inversion**: Depend on abstractions, not concretions. Services receive repositories, `*sqlx.DB`, and adapters via constructor injection (`NewXService(...)`), never globals.

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

## Mandatory: Structured logging

### Go (zerolog)

Every log MUST use `github.com/rs/zerolog/log` — never `fmt.Println`, never stdlib `log`.

| Level | When to use |
|---|---|
| `log.Debug()` | Development-only details: ONVIF URIs, profile tokens, request logs |
| `log.Info()` | Routine operations: snapshot saved, camera scheduled, seed completed |
| `log.Warn()` | Recoverable issues: camera unreachable on one attempt, detector in stub mode |
| `log.Error()` | Operation failures: all capture methods failed, DB init error, job panicked (recovered) |
| `log.Error().Err(err)` | Inside failure paths — attach the error with `.Err(err)` instead of stringifying it |

Use chained fields (never `fmt.Sprintf` into the message): `log.Info().Int64("camera_id", id).Str("status", "ok").Msg("snapshot capture")`.

### Python (loguru)

Every log MUST use `from loguru import logger` — no `print()`, no `logging` stdlib.

| Level | When to use |
|---|---|
| `logger.debug(...)` | Development-only details: ONVIF request/response payloads, raw URIs, profile tokens |
| `logger.info(...)` | Routine operations: snapshot saved, camera scheduled, seed completed |
| `logger.warning(...)` | Recoverable issues: camera unreachable on one attempt, YAML parse fallback, deprecated config |
| `logger.error(...)` | Operation failures: snapshot capture failed, DB init error, ONVIF connection timeout |
| `logger.exception(...)` | Inside `except` blocks only — logs the full traceback automatically |

Use f-string formatting: `logger.info(f"Camera {cam_id} snapshot saved")`.

## Mandatory: Validation & serialization

### Go

- **Input**: DTO structs in `internal/domain/schemas.go` (`json` tags). Manual validation in the handlers (`internal/api/*.go`): required strings, `port` 1–65535, `interval_seconds >= 10`, pagination caps, `Day` for dates. Never trust raw query/path params.
- **Output**: Always serialize domain structs (they carry `json` tags). Never return raw `*sql.Rows` or maps of untyped data from handlers.
- **Timestamps**: Use `domain.SQLTime` (SQLite `YYYY-MM-DD HH:MM:SS` ↔ `time.Time`, emits ISO-8601 in JSON) and `domain.NullSQLTime` for nullable columns.

### Python

- **Input**: Use `StringConstraints` (`min_length`, `strip_whitespace`) and `Field` bounds (`ge`, `le`) on create/update schemas.
- **Output**: Always call `Schema.model_validate(orm_obj)` before returning ORM data.
- **Config**: Always use `ConfigDict(from_attributes=True)` on read schemas.

## Mandatory: Google-style docstrings / doc comments

Every exported symbol in Go and every module, class, function, and method in Python MUST be documented. Use the Google-style format in both languages so the two trees stay consistent.

### Format

```
Single-line summary (max 80 chars, ends with period).

Optionally leave a blank line, then longer description. Sections are
separated by blank lines.
```

### Sections by context

| Context | Required sections |
|---|---|
| **Package / Module** | Summary describing purpose and contents |
| **Type / Class** | Summary + description of responsibility |
| **Function / Method** | Summary, `Args:` (if any params), `Returns:` (if not None), `Raises:` (if any) |
| **HTTP handler / route** | Summary + note of the HTTP method + path |
| **Property** | Summary only (unless complex) |

### Rules

1. **Go**: comment must start with the symbol name (`// Capture runs the three-tier...`) — this is how `gofmt`/`godoc` associate it. Python: `"""` triple double-quotes only.
2. **Summary on first line** — imperative mood ("Get the user", not "Gets the user").
3. **`Args:`** — one line per parameter: `param_name: Description.` Go uses tab-indented blocks; Python uses 4-space indent.
4. **`Returns:`** — describe the return value and its type. Omit if `-> None` / no return value.
5. **`Raises:`** — one line per exception/error: `ValueError: Description of when it occurs.`
6. **Types in docstrings** — do NOT duplicate type annotations; the type hints/signatures serve that purpose.
7. **No docstring is worse than a bad one** — a one-line summary is acceptable for trivial helpers.
