"""Application entry-point for the ONVIF snapshot monitor.

Configures timezone and logging, defines the FastAPI lifespan handler
that initializes the database, seeds cameras, and starts the scheduler,
and mounts all routers and static assets.
"""

import os
import sys
from contextlib import asynccontextmanager
from pathlib import Path

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, HTMLResponse, RedirectResponse
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
from app.scheduler import scheduler, load_schedule


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
    logger.info(f"{settings.app_name} started")
    yield
    logger.info(f"Shutting down {settings.app_name}")
    scheduler.shutdown(wait=False)


app = FastAPI(
    title=settings.app_name,
    lifespan=lifespan,
)

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
    """Serve the dashboard manifest JSON.

    \f
    Returns:
        A FileResponse serving data/manifest.json.

    Raises:
        HTTPException: 404 when the manifest has not been generated yet.
    """
    manifest_path = settings.data_dir / "manifest.json"
    if not manifest_path.exists():
        logger.warning(f"Manifest not found: {manifest_path}")
        raise HTTPException(status_code=404, detail="Manifest not generated yet")
    return FileResponse(str(manifest_path), media_type="application/json")
