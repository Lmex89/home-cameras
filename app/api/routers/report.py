"""FastAPI router for daily report and timelapse video endpoints.

Exposes endpoints to retrieve aggregated daily snapshot reports and
generate downloadable timelapse videos for a camera on a given date.
"""

import shutil
from datetime import date
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import StreamingResponse
from loguru import logger

from app.api.deps import get_snapshot_service
from app.application.services.snapshot_service import SnapshotService
from app.domain.schemas import DailyReport

router = APIRouter(prefix="/api/report", tags=["report"])


@router.get("/{report_date}", response_model=DailyReport)
async def get_report(
    report_date: date,
    service: SnapshotService = Depends(get_snapshot_service),
):
    """Build a daily snapshot report grouped by camera.

    \f
    Args:
        report_date: The date to build the report for.

    Returns:
        A DailyReport containing per-camera snapshot lists and totals.
    """
    logger.debug(f"Report requested for {report_date}")
    report = await service.get_daily_report(report_date)
    logger.info(f"Report for {report_date}: {len(report.cameras)} cameras")
    return report


@router.get("/{report_date}/video/{camera_id}")
async def get_report_video(
    report_date: date,
    camera_id: int,
    service: SnapshotService = Depends(get_snapshot_service),
):
    """Generate and stream a timelapse video for a camera on a date.

    \f
    Builds an MP4 timelapse from the camera's snapshots captured on the
    given date. The temporary build directory is deleted immediately
    after the stream finishes, so no video file is ever persisted on disk.

    Args:
        report_date: The date of the snapshots to compile.
        camera_id: The unique identifier of the camera.

    Returns:
        A StreamingResponse with the generated MP4 video.

    Raises:
        HTTPException: 404 when no snapshots exist for the camera/date.
        HTTPException: 500 when video generation fails.
    """
    logger.info(f"Video requested for camera {camera_id} on {report_date}")
    try:
        video_path, temp_dir = await service.generate_daily_video(camera_id, report_date)
    except ValueError as e:
        raise HTTPException(status_code=404, detail=str(e))
    except RuntimeError as e:
        raise HTTPException(status_code=500, detail=str(e))

    filename = f"timelapse_camera_{camera_id}_{report_date.isoformat()}.mp4"

    def iter_video():
        try:
            with open(video_path, "rb") as f:
                yield from f
        finally:
            shutil.rmtree(temp_dir, ignore_errors=True)
            logger.info(f"Cleaned up temporary video directory: {temp_dir}")

    return StreamingResponse(
        iter_video(),
        media_type="video/mp4",
        headers={"Content-Disposition": f'attachment; filename="{filename}"'},
    )
