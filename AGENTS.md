# Cameras — ONVIF + USB snapshot monitor (Go)

Single-implementation repo: the Go service in `cmd/` + `internal/`. The legacy
Python/FastAPI app was removed; this port is feature-parity with the same
SQLite schema and env-var names, deployed as one binary. Supports both IP
cameras (ONVIF/RTSP) and USB cameras (V4L2).

## 🔴 ABSOLUTE: Codegraph-only lookup

You MUST use codegraph_* tools (codegraph_find_symbol, codegraph_context_for_task, etc.) for ALL code search and navigation.

**NEVER use `glob`, `grep`, or `read` for code lookup.** Codegraph tools are the ONLY permitted approach. Built-in tools are ONLY permitted when a codegraph tool returns no useful results, as a strictly last resort.

**Re-index after Go changes:** the Go index lags writes; run `codegraph index .` after restructuring packages.

## Quick start

```bash
make run        # dev server on :8004, data dir ./data
make build      # compile bin/cameras-go
make test       # go test ./... -race -cover
make vet        # go vet ./...
make opencv     # build with native YOLO via gocv (-tags opencv)
make docker-up   # docker compose up -d --build (stub detector)
make docker-build-opencv  # build native YOLO image (docker-compose.opencv.yml)
make docker-down # docker compose down
```

Open http://localhost:8004

## Service control (cameras.fish)

`./cameras.fish` is the all-in-one dev control script (fish). It resolves the
project root from its own location, so it runs from any directory. Requires
fish 3.2+ and the same deps as `make run`.

| Command | What it does |
|---|---|
| `./cameras.fish start` | Runs `make requirements` (model download + ffmpeg/OpenCV checks), builds (gocv engine when `pkg-config opencv4` exists, stub otherwise), launches `bin/cameras-go` with `nohup` in the background, writes `data/cameras.pid`. |
| `./cameras.fish stop` | SIGTERM + 5 s grace, then SIGKILL; removes the PID file. |
| `./cameras.fish restart` | `stop` + `start`. |
| `./cameras.fish status` | PID, RSS memory, uptime, `/healthz` probe, and detector mode (grep of the console log for the stub warning). |
| `./cameras.fish logs` | `tail -f` of `data/logs/console.log`. |

State artifacts: `data/cameras.pid`, `data/logs/console.log` (both gitignored
under `data/`). Log lines are `[YYYY-MM-DD HH:MM:SS] LEVEL message`. Every
function carries a Google-style docstring (`Args:` / `Returns:` / `Raises:`),
mirroring the Go doc convention in this repo — keep it that way when editing.

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
└── seed/seed.go              # YAML → DB idempotent sync
```

DDD-lite: handlers → services (contain logic) → repositories (data access).
No UnitOfWork: services open `*sqlx.Tx` and pass it to repos. Docs use
Google-style sections (`Args:`, `Returns:`, `Raises:`).

## Key flows

| Step | What happens |
|---|---|
| Startup | init DB from `internal/database/schema.sql` + legacy migrations → seed from `cameras.yaml` → start scheduler (`scheduler.New` + `sched.Start()`) |
| Snapshots | Per-camera interval job → capture tries: IP cameras use direct URL → ONVIF `GetSnapshotUri` → RTSP+ffmpeg; USB cameras use V4L2+ffmpeg → saved to `data/snapshots/{camera_id}/Y/m/d/HMSS.jpg` |
| Analysis  | After each successful capture an `analysis_job` is enqueued; a poller (every 30s) processes pending jobs (`AnalysisService.ProcessNextBatch`). Detector is a singleton; stub mode when the model is missing. |
| Review    | `applyReviewRules` flags snapshots (person after hours, high count, unexpected objects). Surfaces in `/data/manifest.json` and `/api/reviews/pending`. |
| Retention | Daily 06:00 cron → zips raw files older than `SNAPSHOT_ZIP_AFTER_DAYS` into `data/archives/`, deletes records/archives past `SNAPSHOT_RETENTION_DAYS` / `VIDEO_RETENTION_DAYS`. Pauses capture/analysis jobs while running. Also `POST /api/retention/run`. |
| Purge     | Destructive `POST /api/retention/purge`: deletes snapshots, analyses, jobs, videos, archives older than `days` **without** creating archives. |
| Timelapse | Daily cron at `TIMELAPSE_HOUR` generates the annotated MP4 (ffmpeg concat + faststart); uploaded to S3-compatible storage and sent via Telegram. |
| Telegram  | After a timelapse is saved, the MP4 is sent to the configured chat; text+URL fallback above 50 MB. Alarms on scheduled-job panic/failure. |
| Data dirs | `data/` is gitignored, mounted as Docker volume: `cameras.db`, `snapshots/`, `videos/`, `archives/`, `models/`, `logs/`. |

## Config

Env vars via `.env` at the working directory (`config.Load()` via caarlos0/env):

- `APP_NAME`, `DEBUG`, `HOST`, `PORT`, `TIMEZONE`, `SNAPSHOT_RETENTION_DAYS`, `SNAPSHOT_ZIP_AFTER_DAYS`, `VIDEO_RETENTION_DAYS`, `DEFAULT_INTERVAL_SECONDS`, `CAPTURE_TIMEOUT_SECONDS`, `HEALTH_CHECK_INTERVAL_MINUTES`
- `ANALYSIS_ENABLED`, `ANALYSIS_INTERVAL_SECONDS`, `YOLO_MODEL_PATH`, `YOLO_CONFIDENCE_THRESHOLD`, `YOLO_NUM_THREADS`
- `REVIEW_PERSON_AFTER_HOUR`, `REVIEW_PERSON_BEFORE_HOUR`, `REVIEW_MAX_PERSON_COUNT`
- `TELEGRAM_ENABLED`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`
- `TIMELAPSE_HOUR`, `TIMELAPSE_MINUTE`, `TIMELAPSE_CAMERA_ID`, `TIMELAPSE_OBJECT_CLASSES`, `TIMELAPSE_FRAME_DURATION`, `TIMELAPSE_WORKERS`
- `STORAGE_ENABLED`, `STORAGE_ENDPOINT_URL`, `STORAGE_BUCKET_NAME`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY`, `STORAGE_PUBLIC_URL`, `STORAGE_REGION`

## Docker

Plug-and-play `docker compose` (see `docker-compose.yml`): alpine runtime
with ffmpeg, `./data` mounted at `/data`, `.env` optional (`required:
false`), `cameras.yaml` seeded from `cameras.example.yaml` by
`docker/entrypoint.sh` on first start. USB cameras require `devices:`
passthrough in `docker-compose.yml` (already configured for `/dev/video0`
and `/dev/video1`). `Dockerfile` multi-stage: default `BASE=alpine` (stub
detector, pure Go); `BASE=alpine-opencv` + `BUILD_TAGS=opencv` (via
`docker-compose.opencv.yml` overlay) adds OpenCV libs and bakes an
opset-11 yolov8n ONNX export (build-time `model-builder` stage — no
host-side `make model` needed). Keep `BASE=opencv` working as an alias.
SQLite stays the default; see README "When to move off SQLite" for the
PostgreSQL migration guidance.

## Frontend (React + TypeScript)

The SPA lives in `frontend/` — a self-contained Vite + React 18 +
TypeScript project, built as its own Docker image (`cameras-frontend`,
nginx-alpine). The Go binary is API-only: it serves `/api/*` and
`/snapshots/*` (no embedded HTML — the legacy `internal/web/` SPA was
removed). The docker compose stack serves the React app on
`FRONTEND_PORT` (default 3000); nginx proxies `/api/*` and `/snapshots/*`
to the `cameras` service, so the app uses same-origin relative URLs and no
CORS. The upstream port is envsubst'd from `CAMERAS_PORT` (compose passes
`${PORT:-8004}`), which is why the nginx config ships as
`nginx.conf.template`.

| Task | Command |
|---|---|
| Dev server (proxies /api to :8004) | `make frontend-dev` (Node >= 18 required) |
| Build image | `make frontend` or `docker compose build frontend` |
| Run stack | `make docker-up` (backend :8004, frontend :3000) |

Source layout: `src/api/` (typed client mirroring the Go DTOs in
`internal/domain/schemas.go`), `src/pages/Dashboard.tsx` (migrated from
`internal/web/static/index.html`), `src/pages/Reviews.tsx` (from
`reviews.html` — both legacy HTML sources now deleted), `src/components/`
(shared Lightbox/Toast/Ambient, plus
`CameraDashboard.tsx` — the SecureView "Tactical Intelligence"
React/Tailwind dashboard component, previewed at `/design`, designed to be
fed real camera feeds through its typed props), `src/components/MobileDashboard.tsx`
and `src/components/MobileReviews.tsx` — the mobile-first iOS-style
command decks (glass app bar + bottom nav, radar-ping armed card,
single-column live feeds with PTZ/mic overlays, 44px touch targets),
auto-selected below 768px by the Dashboard/Reviews pages via
`src/hooks/useMediaQuery.ts`), `src/styles/` (`tailwind.css` with the
iOS dark-mode theme tokens: black `#000000` canvas, system blue
`#0A84FF` accent, system green `#30D158` safe states, system red
`#FF453A` alerts, glass surfaces `rgb(28 28 30 / 72%)` with
`rgba(255,255,255,0.12)` borders, SF Pro system font stack with tabular
numerals for data; Tailwind component styles only — no preflight — and
both the desktop pages and the mobile decks share this one design
system; legacy CSS files were removed).
PWA assets (manifest.json, favicon) live in `public/`. Data fetching is
plain hooks + fetch (no data-fetch library); the dashboard polls
`/api/data/manifest.json` every 30 s, reviews poll
`/api/reviews/pending` every 15 s.

## Dependencies

- chi/v5, sqlx, modernc.org/sqlite (pure Go, no CGO), caarlos0/env, zerolog, robfig/cron/v3, use-go/onvif, go-telegram-bot-api (via raw HTTP in the notifier), minio-go, fogleman/gg + golang.org/x/image (frame annotation), gopkg.in/yaml.v3, gocv (optional, `-tags opencv`)
- ffmpeg (RTSP frame grab + V4L2 USB capture + timelapse assembly)

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

## Mandatory: Commit hygiene

Every commit MUST follow repository hygiene best practices:

- **Inspect before commit**: run `git status`, `git diff`, and `git diff --cached` before creating a commit.
- **Stage intentionally**: stage only files that belong to the current change; avoid blanket staging when unrelated work exists.
- **No generated artifacts**: do NOT commit build outputs or runtime artifacts (binaries, logs, DB files, caches, temp files, local data dirs).
- **Ignore + untrack correctly**: add generated/local artifacts to `.gitignore`; if already tracked, remove from index with `git rm --cached <path>` and commit that cleanup.
- **Keep commits focused**: prefer small, atomic commits by concern; avoid “commit all pending changes” style commits.

## Mandatory: Structured logging (zerolog)

Every log MUST use `github.com/rs/zerolog/log` — never `fmt.Println`, never stdlib `log`.

| Level | When to use |
|---|---|
| `log.Debug()` | Development-only details: ONVIF URIs, profile tokens, request logs |
| `log.Info()` | Routine operations: snapshot saved, camera scheduled, seed completed |
| `log.Warn()` | Recoverable issues: camera unreachable on one attempt, detector in stub mode |
| `log.Error()` | Operation failures: all capture methods failed, DB init error, job panicked (recovered) |
| `log.Error().Err(err)` | Inside failure paths — attach the error with `.Err(err)` instead of stringifying it |

Use chained fields (never `fmt.Sprintf` into the message): `log.Info().Int64("camera_id", id).Str("status", "ok").Msg("snapshot capture")`.

## Mandatory: Validation & serialization

- **Input**: DTO structs in `internal/domain/schemas.go` (`json` tags). Manual validation in the handlers (`internal/api/*.go`): required strings, `port` 1–65535, `interval_seconds >= 10`, pagination caps, `Day` for dates. Never trust raw query/path params.
- **Output**: Always serialize domain structs (they carry `json` tags). Never return raw `*sql.Rows` or maps of untyped data from handlers.
- **Timestamps**: Use `domain.SQLTime` (SQLite `YYYY-MM-DD HH:MM:SS` ↔ `time.Time`, emits ISO-8601 in JSON) and `domain.NullSQLTime` for nullable columns.

## Mandatory: Google-style doc comments

Every exported symbol MUST be documented. Use the Google-style format.

### Format

```
Single-line summary (max 80 chars, ends with period).

Optionally leave a blank line, then longer description. Sections are
separated by blank lines.
```

### Sections by context

| Context | Required sections |
|---|---|
| **Package** | Summary describing purpose and contents |
| **Type** | Summary + description of responsibility |
| **Function / Method** | Summary, `Args:` (if any params), `Returns:` (if not None), `Raises:` (if any) |
| **HTTP handler / route** | Summary + note of the HTTP method + path |
| **Property** | Summary only (unless complex) |

### Rules

1. Comment must start with the symbol name (`// Capture runs the three-tier...`) — this is how `gofmt`/`godoc` associate it.
2. **Summary on first line** — imperative mood ("Get the user", not "Gets the user").
3. **`Args:`** — one line per parameter: `param_name: Description.` Tab-indented blocks.
4. **`Returns:`** — describe the return value and its type. Omit if no return value.
5. **`Raises:`** — one line per error: `ErrX: Description of when it occurs.`
6. **Types in docstrings** — do NOT duplicate type annotations; the signature serves that purpose.
7. **No docstring is worse than a bad one** — a one-line summary is acceptable for trivial helpers.
