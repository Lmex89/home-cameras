"""Application entry-point for the ONVIF snapshot monitor.

Configures timezone and logging, defines the FastAPI lifespan handler
that initializes the database, seeds cameras, and starts the scheduler,
and mounts all routers and static assets.
"""

import os
import sys
from datetime import datetime
from contextlib import asynccontextmanager
from pathlib import Path

from fastapi import FastAPI, Request
from fastapi.exception_handlers import request_validation_exception_handler
from fastapi.exceptions import RequestValidationError
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import HTMLResponse
from fastapi.staticfiles import StaticFiles
from loguru import logger

from app.core.config import settings

os.environ["TZ"] = settings.timezone
try:
    import time
    time.tzset()
except AttributeError:
    pass

# ── Loguru configuration ──────────────────────────────────────────────
logger.remove()
logger.add(
    sys.stderr,
    format="<green>{time:YYYY-MM-DD HH:mm:ss}</green> | <level>{level: <8}</level> | <cyan>{name}</cyan>:<cyan>{function}</cyan>:<cyan>{line}</cyan> - <level>{message}</level>",
    level="DEBUG" if settings.debug else "INFO",
    colorize=True,
    enqueue=True,
)
log_dir = settings.data_dir / "logs"
log_dir.mkdir(parents=True, exist_ok=True)
logger.add(
    str(log_dir / "app_{time:YYYY-MM-DD}.log"),
    format="{time:YYYY-MM-DD HH:mm:ss.SSS} | {level: <8} | {name}:{function}:{line} - {message}",
    level="DEBUG" if settings.debug else "INFO",
    rotation="1 day",
    retention="7 days",
    compression="zip",
    enqueue=True,
)
# ──────────────────────────────────────────────────────────────────────

from app.core.database import init_db
from app.api.routers import cameras, snapshots, report, videos
from app.web import pages
from app.seed import seed_from_yaml
from app.scheduler import scheduler, load_schedule, schedule_retention


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application startup and shutdown lifecycle.

    Initializes the data directory, applies the SQL schema, seeds cameras
    from YAML, and starts the APScheduler. On shutdown it stops the
    scheduler.

    Args:
        app: The FastAPI application instance.

    Yields:
        Control back to the server while the app is running.
    """
    settings.data_dir.mkdir(parents=True, exist_ok=True)
    schema_path = settings.sql_dir / "schema.sql"
    await init_db(schema_path)
    await seed_from_yaml(settings.yaml_path)
    scheduler.start()
    await load_schedule()
    schedule_retention()
    logger.info(f"{settings.app_name} started")
    yield
    logger.info(f"Shutting down {settings.app_name}")
    scheduler.shutdown(wait=False)


app = FastAPI(
    title=settings.app_name,
    lifespan=lifespan,
)


@app.exception_handler(RequestValidationError)
async def log_validation_error(request: Request, exc: RequestValidationError):
    """Log pydantic validation errors that never reach route handlers."""
    logger.warning(f"Validation error on {request.method} {request.url.path}: {exc.errors()}")
    return await request_validation_exception_handler(request, exc)


app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

static_dir = Path(__file__).resolve().parent / "web" / "static"
app.mount("/static", StaticFiles(directory=str(static_dir)), name="static")

snapshots_dir = settings.snapshots_dir
if snapshots_dir.exists():
    app.mount("/snapshots", StaticFiles(directory=str(snapshots_dir)), name="snapshots")

app.include_router(pages.router)
app.include_router(cameras.router)
app.include_router(snapshots.router)
app.include_router(report.router)
app.include_router(videos.router)


@app.get("/index.html", response_class=HTMLResponse)
async def serve_dashboard_index():
    """Serve the standalone static dashboard page.

    \f
    Returns:
        The contents of the project-root index.html file.
    """
    index_path = Path(__file__).resolve().parent.parent / "index.html"
    return HTMLResponse(content=index_path.read_text(encoding="utf-8"))


@app.get("/data/manifest.json")
async def serve_manifest():
    """Serve a live-generated dashboard manifest from the database.

    \f
    Queries cameras and snapshots on each request so the dashboard
    always shows fresh data without a manual export step.

    Returns:
        JSON manifest with cameras, last snapshots, and per-date
        snapshot lists.
    """
    from app.core.database import engine

    async with engine.connect() as conn:
        rows = (await conn.exec_driver_sql(
            "SELECT id, name, host, port, interval_seconds, enabled "
            "FROM cameras WHERE enabled = 1 ORDER BY id"
        )).mappings().all()

        cameras = []
        for row in rows:
            cam = dict(row)
            last = (await conn.exec_driver_sql(
                "SELECT image_path, captured_at, file_size FROM snapshots "
                "WHERE camera_id = ? AND status = 'success' AND image_path != '' "
                "ORDER BY captured_at DESC LIMIT 1",
                (cam["id"],),
            )).mappings().first()
            cam["last_snapshot"] = {
                "path": last["image_path"],
                "captured_at": last["captured_at"],
                "file_size": last["file_size"],
            } if last else None

            cnt = (await conn.exec_driver_sql(
                "SELECT COUNT(*) as cnt FROM snapshots "
                "WHERE camera_id = ? AND status = 'success'",
                (cam["id"],),
            )).mappings().first()["cnt"]
            cam["total_snapshots"] = cnt
            cameras.append(cam)

        snapshots: dict[str, dict[str, list[dict]]] = {}
        snap_rows = (await conn.exec_driver_sql(
            "SELECT camera_id, image_path, captured_at, file_size FROM snapshots "
            "WHERE status = 'success' AND image_path != '' ORDER BY camera_id, captured_at"
        )).mappings().all()
        for r in snap_rows:
            cam_id = str(r["camera_id"])
            d = r["captured_at"][:10]
            snapshots.setdefault(cam_id, {}).setdefault(d, []).append({
                "path": r["image_path"],
                "captured_at": r["captured_at"],
                "file_size": r["file_size"],
            })

        return {
            "generated_at": datetime.now().isoformat(),
            "cameras": cameras,
            "snapshots": snapshots,
        }
