"""FastAPI router for timelapse video generation endpoints.

Exposes endpoints that render MP4 timelapse videos from a camera's
snapshots for a selected date or hour bucket, including annotated
versions with object detection overlays.
"""

import shutil
from datetime import date
from pathlib import Path

from fastapi import APIRouter, BackgroundTasks, Depends, HTTPException
from fastapi.responses import FileResponse, Response
from loguru import logger

from app.api.deps import get_snapshot_service, get_uow
from app.application.services.snapshot_service import SnapshotService
from app.application.services.timelapse_service import TimelapseService
from app.core.config import settings
from app.core.unit_of_work import UnitOfWork
from app.domain.schemas import VideoRequest, VideoResponse, AnnotatedVideoRequest
from app.infrastructure.archive import read_video_from_archive
from app.infrastructure.storage import StorageProvider
from app.infrastructure.telegram import TelegramNotifier

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

    Falls back to archived ZIP storage if the raw file has been rotated.

    \f
    Args:
        filename: Name of the video file stored in the videos directory.

    Returns:
        A FileResponse serving the MP4 video.

    Raises:
        HTTPException: 404 when the file does not exist.
    """
    path = _videos_dir() / filename
    if path.exists():
        size_mb = path.stat().st_size / (1024 * 1024)
        logger.info(f"Video download: {filename} size={size_mb:.1f}MB")
        return FileResponse(str(path), media_type="video/mp4", filename=filename)

    # Fallback: try archive
    data = read_video_from_archive(filename)
    if data is not None:
        logger.info(f"Video download: {filename} served from archive")
        return Response(content=data, media_type="video/mp4")

    logger.warning(f"Video download: file not found {path}")
    raise HTTPException(status_code=404, detail="Video not found")


async def _notify_telegram(
    video_path: Path, camera_name: str, target_date: date, public_url: str | None = None
) -> None:
    """Send the generated timelapse video to Telegram as a background task.

    Args:
        video_path: Path to the saved MP4 file.
        camera_name: Display name of the camera.
        target_date: Date the timelapse covers.
        public_url: Optional public (Blaze) URL to include as a text message.
    """
    size_mb = video_path.stat().st_size / (1024 * 1024)
    caption = f"\U0001f3a5 Camera {camera_name} — timelapse {target_date.isoformat()} (annotated, {size_mb:.1f} MB)"
    notifier = TelegramNotifier.from_settings()
    await notifier.send_video(
        video_path,
        caption=caption,
        fallback_url=f"http://localhost:{settings.port}/api/videos/download/{video_path.name}",
        public_url=public_url,
    )


@router.post("/annotated", response_model=VideoResponse)
async def create_annotated_video(
    payload: AnnotatedVideoRequest,
    background_tasks: BackgroundTasks,
    uow: UnitOfWork = Depends(get_uow),
):
    """Generate an annotated timelapse MP4 with object detection overlays.

    Draws bounding boxes for the configured object classes (person, car,
    motorcycle by default) on each snapshot frame before assembling the
    video. Snapshots without analysis results are included without boxes.
    After generation, the video is sent to Telegram as a background task.

    \f
    Args:
        payload: Request body containing camera_id, date, and optional
            classes override (comma-separated).
        background_tasks: FastAPI background task queue.
        uow: Unit of Work injected by dependency.

    Returns:
        A VideoResponse with the download URL for the generated MP4.

    Raises:
        HTTPException: 400 on invalid input or missing snapshots,
            500 when ffmpeg fails.
    """
    camera_id = payload.camera_id
    target_date = payload.date
    target_classes = set(payload.classes.split(",")) if payload.classes else None
    logger.info(f"Annotated video requested: camera={camera_id} date={target_date} classes={target_classes}")

    camera = await uow.cameras.get_by_id(camera_id)
    if not camera:
        raise HTTPException(status_code=404, detail="Camera not found")

    svc = TimelapseService(uow)
    try:
        output_path, temp_dir = await svc.generate_annotated_timelapse(
            camera_id, target_date, target_classes=target_classes,
        )
    except ValueError as e:
        logger.warning(f"Annotated video request rejected: camera={camera_id} reason={e}")
        raise HTTPException(status_code=400, detail=str(e))
    except (RuntimeError, OSError, IsADirectoryError) as e:
        logger.exception(f"Annotated video generation failed: camera={camera_id}")
        raise HTTPException(status_code=500, detail=str(e))

    vdir = _videos_dir()
    persistent = vdir / f"timelapse_annotated_{camera_id}_{target_date.isoformat()}.mp4"
    shutil.move(str(output_path), str(persistent))
    shutil.rmtree(temp_dir, ignore_errors=True)
    logger.info(f"Video saved to disk: {persistent}")

    # Upload to Blaze (Backblaze B2) and return public URL
    logger.info("Uploading to Backblaze B2...")
    storage = StorageProvider.from_settings()
    if storage:
        blaze_url = await storage.upload(persistent)
    else:
        blaze_url = None

    background_tasks.add_task(_notify_telegram, persistent, camera.name, target_date, blaze_url)
    logger.info("Telegram notification scheduled as background task")

    if blaze_url:
        url = blaze_url
        logger.info(f"Annotated video uploaded to Blaze: {url}")
    else:
        url = f"/api/videos/download/{persistent.name}"
        logger.info(f"Blaze upload unavailable, using local URL: {url}")

    return VideoResponse(video_url=url)
