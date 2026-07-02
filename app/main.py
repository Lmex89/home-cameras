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
from app.core.unit_of_work import UnitOfWork
from app.domain.schemas import CameraRead, CameraWithLastSnapshot, SnapshotRead

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
from app.api.routers import cameras, snapshots, report, videos, reviews
from app.web import pages
from app.seed import seed_from_yaml
from app.scheduler import scheduler, load_schedule, schedule_retention, schedule_analysis


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
    schedule_analysis()
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
app.include_router(reviews.router)


@app.get("/index.html", response_class=HTMLResponse)
async def serve_dashboard_index():
    """Serve the standalone static dashboard page.

    \f
    Returns:
        The contents of the project-root index.html file.
    """
    index_path = Path(__file__).resolve().parent.parent / "index.html"
    return HTMLResponse(content=index_path.read_text(encoding="utf-8"))


@app.get("/reviews.html", response_class=HTMLResponse)
async def serve_reviews_page():
    """Serve the standalone review dashboard page.

    \f
    Returns:
        The contents of the project-root reviews.html file.
    """
    import os
    path = Path(__file__).resolve().parent.parent / "reviews.html"
    if not path.exists():
        return HTMLResponse("<h1>Not Found</h1><p>reviews.html not deployed</p>", status_code=404)
    return HTMLResponse(content=path.read_text(encoding="utf-8"))


@app.get("/data/manifest.json")
async def serve_manifest():
    """Serve a live dashboard manifest generated from the database.

    Queries all enabled cameras and their successful snapshots on
    every request so the dashboard always shows fresh data without
    requiring a manual export step.

    Returns:
        Dict with ``generated_at`` timestamp, ``cameras`` list (each
        including ``last_snapshot`` and ``total_snapshots``), and
        ``snapshots`` dict keyed by camera_id → date → snapshot list.
    """
    from app.core.database import session_factory as _sf

    async with UnitOfWork(_sf) as uow:
        cameras = await uow.cameras.get_enabled()
        camera_ids = [c.id for c in cameras]

        last_snapshots = await uow.snapshots.get_last_for_all_cameras(camera_ids)
        all_snaps = await uow.snapshots.get_all_successful_with_images()

        # Build total_snapshots count per camera from the full list
        total_by_cam: dict[int, int] = {}
        for snap in all_snaps:
            total_by_cam[snap.camera_id] = total_by_cam.get(snap.camera_id, 0) + 1

        cameras_data = []
        for cam in cameras:
            cam_schema = CameraWithLastSnapshot(
                **CameraRead.model_validate(cam).model_dump(),
                last_snapshot=SnapshotRead.model_validate(last_snapshots[cam.id])
                if last_snapshots.get(cam.id)
                else None,
                total_snapshots=total_by_cam.get(cam.id, 0),
            )
            cameras_data.append(cam_schema.model_dump())

        snapshots: dict[str, dict[str, list[dict]]] = {}
        for snap in all_snaps:
            cam_id = str(snap.camera_id)
            d = snap.captured_at.strftime("%Y-%m-%d")
            snap_dict = SnapshotRead.model_validate(snap).model_dump(
                include={"image_path", "captured_at", "file_size"}
            )
            snapshots.setdefault(cam_id, {}).setdefault(d, []).append(snap_dict)

        from app.application.services.analysis_service import AnalysisService
        analysis_service = AnalysisService(uow)
        pending_reviews = await analysis_service.get_pending_reviews(limit=100)
        review_count = len(pending_reviews)

        logger.info(
            f"Manifest generated: {len(cameras_data)} cameras, "
            f"{sum(total_by_cam.values())} snapshots, "
            f"{review_count} pending reviews"
        )

        return {
            "generated_at": datetime.now().isoformat(),
            "cameras": cameras_data,
            "snapshots": snapshots,
            "pending_reviews": pending_reviews[:10],
            "review_count": review_count,
        }
