"""APScheduler-based snapshot capture and retention scheduler.

Defines the shared AsyncIOScheduler and helpers to schedule, remove,
and reschedule per-camera capture jobs at fixed intervals, as well
as a daily retention cleanup job.
"""

import shutil
from datetime import date, datetime, timedelta
from zoneinfo import ZoneInfo

from loguru import logger
from apscheduler.schedulers.asyncio import AsyncIOScheduler
from apscheduler.triggers.cron import CronTrigger
from apscheduler.triggers.interval import IntervalTrigger

from app.core.config import settings
from app.core.database import session_factory
from app.core.unit_of_work import UnitOfWork
from app.infrastructure.onvif import ONVIFCameraClient
from app.infrastructure.telegram import TelegramNotifier
from app.application.services.snapshot_service import SnapshotService

scheduler = AsyncIOScheduler(timezone=settings.timezone)


async def capture_job(camera_id: int) -> None:
    """Run a single snapshot capture for the given camera.

    Loads the camera from the database, skipping disabled or missing
    cameras, and invokes the SnapshotService to capture a snapshot.

    Args:
        camera_id: The unique identifier of the camera to capture.
    """
    async with UnitOfWork(session_factory) as uow:
        camera = await uow.cameras.get_by_id(camera_id)
        if not camera or not camera.enabled:
            return
        onvif = ONVIFCameraClient()
        service = SnapshotService(uow, onvif)
        try:
            snapshot = await service.capture(camera)
            status = "ok" if snapshot.status == "success" else "error"
            logger.info(f"Camera {camera.name} ({camera.host}): snapshot {status}")
        except Exception:
            logger.exception(f"Camera {camera.name}: capture failed")


def schedule_camera(
    camera_id: int,
    interval_seconds: int,
    start_date: datetime | None = None,
) -> None:
    """Schedule a recurring capture job for a camera.

    Args:
        camera_id: The unique identifier of the camera.
        interval_seconds: Seconds between consecutive captures.
        start_date: Optional anchor datetime for the interval. When None,
            the interval starts from the current time.
    """
    job_id = f"capture_{camera_id}"
    trigger_kwargs = {"seconds": interval_seconds}
    if start_date is not None:
        trigger_kwargs["start_date"] = start_date
    scheduler.add_job(
        capture_job,
        trigger=IntervalTrigger(**trigger_kwargs),
        args=[camera_id],
        id=job_id,
        replace_existing=True,
        name=f"Capture {camera_id} every {interval_seconds}s",
    )
    logger.info(f"Scheduled camera {camera_id} every {interval_seconds} sec")


def remove_camera_job(camera_id: int) -> None:
    """Remove the scheduled capture job for a camera if it exists.

    Args:
        camera_id: The unique identifier of the camera.
    """
    job_id = f"capture_{camera_id}"
    if scheduler.get_job(job_id):
        scheduler.remove_job(job_id)
        logger.info(f"Removed job for camera {camera_id}")


def reschedule_camera(
    camera_id: int,
    interval_seconds: int,
    start_date: datetime | None = None,
) -> None:
    """Remove and re-add a camera's capture job with a new interval.

    Args:
        camera_id: The unique identifier of the camera.
        interval_seconds: The new interval in seconds between captures.
        start_date: Optional anchor datetime for the interval.
    """
    remove_camera_job(camera_id)
    schedule_camera(camera_id, interval_seconds, start_date)


async def load_schedule() -> None:
    """Schedule capture jobs for all currently enabled cameras.

    Anchors each camera's interval to its most recent successful snapshot
    so the schedule survives server restarts. Cameras without snapshots
    start from the current time.
    """
    async with UnitOfWork(session_factory) as uow:
        cameras = await uow.cameras.get_enabled()
        for cam in cameras:
            last_snap = await uow.snapshots.get_last_by_camera(cam.id)
            start_date = last_snap.captured_at if last_snap else datetime.now()
            schedule_camera(cam.id, cam.interval_seconds, start_date)


async def retention_job() -> None:
    """Run daily retention cleanup (zip old files, delete expired).

    Pauses capture and analysis jobs during retention to avoid SQLite
    write contention — only one writer is allowed at a time.
    """
    logger.info("Retention job: pausing capture/analysis schedulers")
    capture_ids = [j.id for j in scheduler.get_jobs() if j.id.startswith("capture_")]
    analysis_id = "analysis_processing"
    for jid in capture_ids:
        scheduler.pause_job(jid)
    scheduler.pause_job(analysis_id)

    try:
        async with UnitOfWork(session_factory) as uow:
            from app.application.services.retention_service import RetentionService
            service = RetentionService(uow)
            result = await service.run()
            logger.info(f"Retention job complete: {result}")
    except Exception:
        logger.exception("Retention job failed")
    finally:
        logger.info("Retention job: resuming capture/analysis schedulers")
        for jid in capture_ids:
            scheduler.resume_job(jid)
        scheduler.resume_job(analysis_id)


async def analysis_job() -> None:
    """Process pending analysis jobs from the queue."""
    logger.debug("analysis_job fired")
    try:
        async with UnitOfWork(session_factory) as uow:
            from app.application.services.analysis_service import AnalysisService
            service = AnalysisService(uow)
            processed = await service.process_next_batch(limit=5)
            if processed:
                logger.info(f"Analysis batch processed: {processed} jobs")
            else:
                logger.debug("Analysis batch: no pending jobs")
    except Exception:
        logger.exception("Analysis job failed")


def schedule_analysis() -> None:
    """Schedule periodic analysis job processing."""
    scheduler.add_job(
        analysis_job,
        trigger=IntervalTrigger(seconds=settings.analysis_interval_seconds),
        id="analysis_processing",
        replace_existing=True,
        name="Analysis job processing",
    )
    logger.info(f"Scheduled analysis processing every {settings.analysis_interval_seconds}s")


def schedule_retention() -> None:
    """Schedule the daily retention cleanup job at 06:00 local time."""
    scheduler.add_job(
        retention_job,
        trigger=CronTrigger(hour=6, minute=0, timezone=ZoneInfo(settings.timezone)),
        id="retention_cleanup",
        replace_existing=True,
        name="Daily retention cleanup",
    )
    logger.info("Scheduled daily retention cleanup at 06:00 (local time)")


async def timelapse_job() -> None:
    """Generate the daily annotated timelapse for the previous day."""
    yesterday = date.today() - timedelta(days=1)
    camera_id = settings.timelapse_camera_id
    logger.info(f"Timelapse job: generating annotated timelapse for camera {camera_id} on {yesterday}")
    camera_name = str(camera_id)
    try:
        async with UnitOfWork(session_factory) as uow:
            camera = await uow.cameras.get_by_id(camera_id)
            if camera:
                camera_name = camera.name
            from app.application.services.timelapse_service import TimelapseService
            svc = TimelapseService(uow)
            output_path, temp_dir = await svc.generate_annotated_timelapse(camera_id, yesterday)
        vdir = settings.videos_dir
        vdir.mkdir(parents=True, exist_ok=True)
        persistent = vdir / f"timelapse_annotated_{camera_id}_{yesterday.isoformat()}.mp4"
        shutil.move(str(output_path), str(persistent))
        shutil.rmtree(temp_dir, ignore_errors=True)
        logger.info(f"Annotated timelapse saved: {persistent}")

        from app.infrastructure.storage import StorageProvider
        storage = StorageProvider.from_settings()
        blaze_url = await storage.upload(persistent) if storage else None

        size_mb = persistent.stat().st_size / (1024 * 1024)
        caption = f"\U0001f3a5 Camera {camera_name} — timelapse {yesterday.isoformat()} (annotated, {size_mb:.1f} MB)"
        notifier = TelegramNotifier.from_settings()
        await notifier.send_video(
            persistent,
            caption=caption,
            fallback_url=f"http://localhost:{settings.port}/api/videos/download/{persistent.name}",
            public_url=blaze_url,
        )
    except Exception:
        logger.exception(f"Timelapse job failed for camera {camera_id} on {yesterday}")


def schedule_timelapse() -> None:
    """Schedule the daily annotated timelapse generation job."""
    if not settings.timelapse_enabled:
        logger.info("Timelapse generation disabled via TIMELAPSE_ENABLED=false")
        return
    scheduler.add_job(
        timelapse_job,
        trigger=CronTrigger(
            hour=settings.timelapse_hour,
            minute=settings.timelapse_minute,
            timezone=ZoneInfo(settings.timezone),
        ),
        id="timelapse_generation",
        replace_existing=True,
        name="Daily annotated timelapse generation",
    )
    logger.info(f"Scheduled daily annotated timelapse at {settings.timelapse_hour:02d}:{settings.timelapse_minute:02d} (local time)")
