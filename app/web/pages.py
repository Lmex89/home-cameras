from pathlib import Path

from fastapi import APIRouter, Depends, Request
from fastapi.responses import HTMLResponse
from fastapi.templating import Jinja2Templates
from loguru import logger

from app.api.deps import get_camera_service, get_snapshot_service
from app.application.services.camera_service import CameraService
from app.application.services.snapshot_service import SnapshotService

templates = Jinja2Templates(directory=Path(__file__).resolve().parent / "templates")
router = APIRouter(tags=["pages"])


@router.get("/", response_class=HTMLResponse)
async def dashboard(
    request: Request,
    service: SnapshotService = Depends(get_snapshot_service),
):
    data = await service.get_dashboard_data()
    logger.debug(f"Rendering dashboard with {len(data)} cameras")
    return templates.TemplateResponse(
        request, "dashboard.html", {"cameras": data},
    )


@router.get("/report", response_class=HTMLResponse)
async def report_page(request: Request):
    logger.debug("Rendering report page")
    return templates.TemplateResponse(
        request, "report.html",
    )


@router.get("/cameras", response_class=HTMLResponse)
async def cameras_page(
    request: Request,
    service: CameraService = Depends(get_camera_service),
):
    cameras = await service.list_cameras()
    logger.debug(f"Rendering cameras page with {len(cameras)} cameras")
    return templates.TemplateResponse(
        request, "cameras.html", {"cameras": cameras},
    )
