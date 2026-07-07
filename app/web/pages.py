"""FastAPI router serving server-rendered Jinja2 web pages.

Renders the dashboard, report, and cameras management pages backed by
Jinja2 templates.
"""

from pathlib import Path

from fastapi import APIRouter, Depends, Request
from fastapi.responses import HTMLResponse, RedirectResponse
from fastapi.templating import Jinja2Templates
from loguru import logger

from app.api.deps import get_camera_service
from app.application.services.camera_service import CameraService

templates = Jinja2Templates(directory=Path(__file__).resolve().parent / "templates")
router = APIRouter(tags=["pages"])


@router.get("/")
async def dashboard():
    """Redirect the root path to the static dashboard.

    The standalone dashboard in index.html provides pagination, hour
    grouping, and video generation, so the old Jinja2 dashboard is no
    longer the primary entry point.

    Returns:
        A 307 redirect to /index.html.
    """
    return RedirectResponse(url="/index.html")


@router.get("/report", response_class=HTMLResponse)
async def report_page(request: Request):
    """Render the report selection page.

    \f
    Args:
        request: The incoming HTTP request.

    Returns:
        An HTMLResponse rendering report.html.
    """
    logger.debug("Rendering report page")
    return templates.TemplateResponse(
        request, "report.html",
    )


@router.get("/cameras", response_class=HTMLResponse)
async def cameras_page(
    request: Request,
    service: CameraService = Depends(get_camera_service),
):
    """Render the cameras management page.

    \f
    Args:
        request: The incoming HTTP request.

    Returns:
        An HTMLResponse rendering cameras.html with the camera list.
    """
    cameras = await service.list_cameras()
    logger.debug(f"Rendering cameras page with {len(cameras)} cameras")
    return templates.TemplateResponse(
        request, "cameras.html", {"cameras": cameras},
    )
