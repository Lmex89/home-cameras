arkdown

# Cameras — ONVIF Snapshot Monitor Specification & Implementation Plan

## Architectural Mandates & Guidelines

### Codegraph Code Search Protocol
* All code search and navigation MUST exclusively use `codegraph_*` tools (`codegraph_find_symbol`, `codegraph_context_for_task`, `codegraph_callers`, `codegraph_callees`, `codegraph_impact`, `codegraph_search`).
* Standard built-in search tools (`grep`, `glob`, file reading for discovery) are prohibited unless a `codegraph` query returns an empty result set as a strict last resort.

### SOLID Architectural Principles
* **Single Responsibility (SRP):** Classes maintain one clear purpose. `CameraService` handles camera CRUD, `SnapshotService` executes multi-protocol image capture, `ONVIFCameraClient` isolates wire protocols, `RetentionService` manages filesystem lifecycle.
* **Open/Closed (OCP):** Extend system capabilities via modular adapters. Abstract base classes (such as `BaseMLDetector`) allow adding inference engines (e.g., TensorRT, OpenVINO) without modifying existing domain logic.
* **Liskov Substitution (LSP):** All repository implementations inherit from uniform abstract interfaces guaranteeing identical query, storage, and transaction semantics.
* **Interface Segregation (ISP):** Domain interfaces expose only client-required methods rather than generic monolithic interfaces.
* **Dependency Inversion (DIP):** Application layers depend on abstractions. Concrete services receive `UnitOfWork` and hardware client interfaces through constructor injection.

### Code Quality & Standards
* **Conventional Commits:** Adhere strictly to `conventionalcommits.org` v1.0.0 (`feat`, `fix`, `build`, `chore`, `ci`, `docs`, `style`, `refactor`, `perf`, `test`, `revert`). Breaking changes require `!` or `BREAKING CHANGE:` footers.
* **Structured Logging:** Use `from loguru import logger` exclusively. Never use stdlib `logging` or `print()`. Use f-string formatting (`logger.info(f"...")`) and `logger.exception()` within `except` blocks to capture stack trace contexts automatically.
* **Validation & Serialization:** All API routes enforce Pydantic v2 schemas. Input parameters use `StringConstraints` and `Field` bounds. Output entities explicitly execute `Schema.model_validate(orm_obj)`. Configuration classes inherit from `pydantic-settings`.
* **Docstrings:** All modules, classes, functions, and path operations require Google-style docstrings. Imperative summary on line 1, detailed `Args:`, `Returns:`, and `Raises:` sections where applicable.

---

## Directory Architecture

```text
app/
├── main.py                  # FastAPI app lifespan (DB init, WAL PRAGMAs, scheduler)
├── core/
│   ├── config.py            # pydantic-settings configuration from .env
│   ├── database.py          # SQLAlchemy async engine, sqlite PRAGMAs, sessionmaker
│   └── unit_of_work.py      # Async UnitOfWork implementation
├── domain/
│   ├── models/              # SQLAlchemy ORM models (Camera, Snapshot, AnalysisJob, SnapshotAnalysis)
│   └── schemas/             # Pydantic v2 request/response validation schemas
├── application/
│   ├── services/
│   │   ├── snapshot_service.py   # Multi-protocol snapshot ingestion & storage
│   │   ├── camera_service.py     # Camera administration CRUD
│   │   ├── analysis_service.py   # Async YOLO inference & heuristic review engine
│   │   └── retention_service.py  # Lock-safe retention cleanup & compression
│   └── repositories/             # Async repository interfaces & implementations
├── infrastructure/
│   ├── onvif.py              # ONVIFCameraClient protocol adapter
│   ├── storage.py            # Local filesystem & S3/B2 provider abstractions
│   ├── telegram.py           # TelegramNotifier for automated video alerts
│   └── ml/
│       ├── adapter.py        # Abstract base class for computer vision engines
│       └── yolo.py           # YOLO adapter with CPU offloading & graceful stub fallback
├── api/
│   ├── dependencies.py       # Async UnitOfWork dependency injection
│   └── routers/              # Route handlers (cameras, snapshots, reports, videos, reviews, health)
├── web/                      # Jinja2 web interface templates & static assets
├── sql/schema.sql            # Raw SQL DDL schema for DB initialization
├── scheduler.py              # APScheduler lifecycle & job definitions
└── seed.py                   # YAML-to-SQLite idempotent data seeder
tests/
├── unit/                     # Domain & service logic isolation tests
├── integration/              # Async SQLite repository & endpoint integration tests
└── fixtures/                 # Sample snapshot frames, XML payloads, YAML mocks


Concurrency & System Upgrades

SQLite WAL & Lock Mitigation

High-frequency concurrent background writes (ingestion runs, ML state changes, retention purges) must execute without triggering SQLITE_BUSY contention:

Connection PRAGMAs: Configure connection hooks on the async SQLAlchemy engine (core/database.py):

PRAGMA journal_mode=WAL; (Enables concurrent reads alongside active writes).

PRAGMA busy_timeout=5000; (Waits up to 5000ms for locks to clear before raising errors).

PRAGMA synchronous=NORMAL; (Optimizes write latency with full WAL safety).

Transaction Isolation: Keep UnitOfWork async transaction scopes small. Perform network operations (ONVIF requests, HTTP pulls) and filesystem writes outside active database transactions.

Event Loop Isolation

Subprocess Offloading: Subprocess execution (such as ffmpeg RTSP frame captures or timelapse builds) must be offloaded using asyncio.to_thread(...) to avoid stalling the FastAPI event loop.

Inference Offloading: Model inference (ultralytics YOLO execution) is CPU/GPU bound and blocking. Wrap image evaluations in asyncio.to_thread(self.model.predict, image_path).

Atomic File Storage: File operations in data/snapshots/ write to a temporary file (.tmp) prior to performing an atomic os.replace operation, avoiding file lock collisions during background analysis or archiving.

Subsystem Workflows

Ingestion Protocol Chain (SnapshotService)

Fallback Priority: Direct Image URL -> ONVIF GetSnapshotUri -> RTSP frame grab via ffmpeg.

Failure Management: Record transient errors with logger.warning(). If all protocols fail, record a failed attempt in the database without throwing unhandled exceptions, maintaining schedule continuity.

Storage Format: Write raw snapshots using the temporal folder layout: data/snapshots/{camera_id}/{YYYY}/{MM}/{DD}/{HH}{MM}{SS}.jpg.

ML Analysis & Heuristic Review (AnalysisService)

Batch Polling: APScheduler queries PENDING items every 30 seconds. State updates process atomically within unit-of-work transactions.

Fallback Execution: If ultralytics or model parameters are missing, the system automatically falls back to an internal stub detector, setting detection results to empty sets and logging a warning.

Rule Evaluator (_apply_review_rules):

Flag snapshots containing persons between REVIEW_PERSON_AFTER_HOUR and REVIEW_PERSON_BEFORE_HOUR.

Flag snapshots exceeding REVIEW_MAX_PERSON_COUNT.

Flag unexpected object classes based on camera configurations.

Retention & Archival Lifecycle (RetentionService)

Execution Schedule: Invoked via daily 06:00 APScheduler job or fish run-retention.fish.

Archival Routine:

Compress raw snapshots older than SNAPSHOT_ZIP_AFTER_DAYS into zip files under data/archives/.

Delete original snapshot files after successful compression.

Purge database records and archive files exceeding SNAPSHOT_RETENTION_DAYS.

Purge generated video files exceeding VIDEO_RETENTION_DAYS.

Service Management Commands

Fragmento de código

# Development server startup
uvicorn app.main:app --reload --port 8004

# Docker containerization
docker compose up --build

# Service installation & control (Production Bare-Metal)
sudo fish manage-service.fish install
sudo fish manage-service.fish start
sudo fish manage-service.fish stop
sudo fish manage-service.fish restart
sudo fish manage-service.fish status
sudo fish manage-service.fish logs

# Hot reload development server script
fish startup.fish --restart

# Retention cleanup execution
fish run-retention.fish

# Destructive purge execution (older than N days)
fish run-purge.fish

# Manual timelapse video generation
fish run-timelapse.fish


Implementation Roadmap

Phase 1: Core Architecture & Database Layer

Build core/config.py using pydantic-settings to process .env variables.

Implement core/database.py using aiosqlite, setting up connection listeners for SQLite WAL, busy_timeout, and synchronous PRAGMAs.

Define SQLAlchemy ORM entities in domain/models/ and Pydantic schemas in domain/schemas/.

Construct abstract and concrete repositories alongside core/unit_of_work.py.

Create seed.py to parse cameras.yaml and idempotently seed camera metadata.

Phase 2: Capture Engine & ONVIF Integration

Implement infrastructure/onvif.py to manage connection timeouts and SOAP calls to IP cameras.

Build SnapshotService with three-tier protocol fallback execution and thread-offloaded ffmpeg integration.

Configure scheduler.py using APScheduler to trigger snapshot captures on camera schedules.

Phase 3: Machine Learning & Review Subsystem

Construct infrastructure/ml/yolo.py with asyncio.to_thread execution wrappers and stub fallback logic.

Build AnalysisService to consume queue items, record detections, and process heuristics inside _apply_review_rules().

Construct FastAPI router endpoints in api/routers/reviews.py to serve pending review items.

Phase 4: Lifecycle Automation & Video Generation

Implement RetentionService providing lock-safe file compression and cleanup routines.

Build infrastructure/telegram.py with automated payload size verification (>50MB triggers fallback URL formatting).

Implement ffmpeg MP4 video generation for daily timelapse notifications.

Verify execution of manage-service.fish, run-retention.fish, and run-purge.fish.

Phase 5: API Layer, Observability & Verification

Finalize FastAPI routers (cameras.py, snapshots.py, report.py, videos.py, reviews.py) enforcing complete schema validation on inputs and outputs.

Construct GET /healthz endpoint returning DB connection status, storage space metrics, camera reachability stats, and scheduler status.

Build complete test suite in tests/: unit tests with mocked infrastructure, integration tests running against in-memory async SQLite, and HTTP route assertions via httpx.AsyncClient.
