import sys
from contextlib import asynccontextmanager
from pathlib import Path

from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles
from loguru import logger

from app.core.config import settings

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
from app.api.routers import cameras, snapshots, report
from app.web import pages
from app.seed import seed_from_yaml
from app.scheduler import scheduler, load_schedule


@asynccontextmanager
async def lifespan(app: FastAPI):
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

static_dir = Path(__file__).resolve().parent / "web" / "static"
app.mount("/static", StaticFiles(directory=str(static_dir)), name="static")

snapshots_dir = settings.snapshots_dir
if snapshots_dir.exists():
    app.mount("/snapshots", StaticFiles(directory=str(snapshots_dir)), name="snapshots")

app.include_router(pages.router)
app.include_router(cameras.router)
app.include_router(snapshots.router)
app.include_router(report.router)
