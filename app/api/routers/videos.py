"""FastAPI router for timelapse video generation endpoints.

Exposes endpoints that render MP4 timelapse videos from a camera's
snapshots for a selected date or hour bucket.
"""

import shutil
from datetime import date
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import FileResponse
from loguru import logger

from app.api.deps import get_snapshot_service
from app.application.services.snapshot_service import SnapshotService
from app.core.config import settings
from app.domain.schemas import VideoRequest, VideoResponse

router = APIRouter(prefix="/api/videos", tags=["videos"])


def _videos_dir() -> Path:
    """Return the persistent directory for generated videos."""
    vdir = settings.data_dir / "videos"
    vdir.mkdir(parents=True, exist_ok=True)
    return vdir


@router.post("", response_model=VideoResponse)
async def create_video(
    payload: VideoRequest,
    service: SnapshotService = Depends(get_snapshot_service),
):
    """Generate a timelapse MP4 from snapshots.

    \f
    Args:
        payload: Request body containing camera_id, date, and optional
            hour (0-23).
        service: SnapshotService injected by dependency.

    Returns:
        A VideoResponse with the download URL for the generated MP4.

    Raises:
        HTTPException: 400 on invalid input, 404 when the camera or
            snapshots are missing, 500 when ffmpeg fails.
    """
    hour_label = f"hour {payload.hour:02d}" if payload.hour is not None else "all day"
    logger.info(f"Video requested: camera={payload.camera_id} date={payload.date} {hour_label}")

    camera = await service._uow.cameras.get_by_id(payload.camera_id)
    if not camera:
        logger.warning(f"Video request: camera {payload.camera_id} not found")
        raise HTTPException(status_code=404, detail="Camera not found")

    try:
        output_path, temp_dir = await service.generate_daily_video(
            payload.camera_id, payload.date, payload.hour
        )
    except ValueError as e:
        logger.warning(f"Video request rejected: camera={payload.camera_id} reason={e}")
        raise HTTPException(status_code=400, detail=str(e))
    except RuntimeError as e:
        logger.exception(f"Video generation failed: camera={payload.camera_id}")
        raise HTTPException(status_code=500, detail=str(e))

    vdir = _videos_dir()
    suffix = f"_h{payload.hour:02d}" if payload.hour is not None else ""
    persistent = vdir / f"timelapse_{payload.camera_id}_{payload.date.isoformat()}{suffix}.mp4"
    shutil.move(str(output_path), str(persistent))
    shutil.rmtree(temp_dir, ignore_errors=True)

    url = f"/api/videos/download/{persistent.name}"
    logger.info(f"Video saved: {persistent} url={url}")
    return VideoResponse(video_url=url)


@router.get("/download/{filename}")
async def download_video(filename: str):
    """Download a previously generated MP4 video by filename.

    \f
    Args:
        filename: Name of the video file stored in the videos directory.

    Returns:
        A FileResponse serving the MP4 video.

    Raises:
        HTTPException: 404 when the file does not exist.
    """
    path = _videos_dir() / filename
    if not path.exists():
        logger.warning(f"Video download: file not found {path}")
        raise HTTPException(status_code=404, detail="Video not found")
    size_mb = path.stat().st_size / (1024 * 1024)
    logger.info(f"Video download: {filename} size={size_mb:.1f}MB")
    return FileResponse(str(path), media_type="video/mp4", filename=filename)
