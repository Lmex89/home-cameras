"""FastAPI router for daily report and timelapse video endpoints.

Exposes endpoints to retrieve aggregated daily snapshot reports and
generate downloadable timelapse videos for a camera on a given date.
"""

import shutil
from datetime import date

from fastapi import APIRouter, BackgroundTasks, Depends, HTTPException
from fastapi.responses import FileResponse
from loguru import logger

from app.api.deps import get_snapshot_service
from app.application.services.snapshot_service import SnapshotService
from app.domain.schemas import DailyReport, DailyReportCamera, SnapshotRead

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
    raw = await service.get_daily_report(report_date)
    cameras_list = []
    for cam_id, data in raw["cameras"].items():
        snapshots = [SnapshotRead.model_validate(s) for s in data["snapshots"]]
        cameras_list.append(
            DailyReportCamera(
                camera_id=cam_id,
                camera_name=data["name"],
                total_snapshots=len(snapshots),
                snapshots=snapshots,
            )
        )
    logger.info(f"Report for {report_date}: {len(cameras_list)} cameras")
    return DailyReport(date=raw["date"], cameras=cameras_list)


@router.get("/{report_date}/video/{camera_id}")
async def get_report_video(
    report_date: date,
    camera_id: int,
    background_tasks: BackgroundTasks,
    service: SnapshotService = Depends(get_snapshot_service),
):
    """Generate and download a timelapse video for a camera on a date.

    \f
    Builds an MP4 timelapse from the camera's snapshots captured on the
    given date. The temporary build directory is cleaned up via a
    background task after the response completes.

    Args:
        report_date: The date of the snapshots to compile.
        camera_id: The unique identifier of the camera.
        background_tasks: FastAPI background task runner for cleanup.

    Returns:
        A FileResponse streaming the generated MP4 video.

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

    background_tasks.add_task(lambda: shutil.rmtree(temp_dir, ignore_errors=True))

    filename = f"timelapse_camera_{camera_id}_{report_date.isoformat()}.mp4"
    return FileResponse(
        str(video_path),
        media_type="video/mp4",
        filename=filename,
        headers={"Content-Disposition": f'attachment; filename="{filename}"'},
    )
