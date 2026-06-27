from loguru import logger
from apscheduler.schedulers.asyncio import AsyncIOScheduler
from apscheduler.triggers.interval import IntervalTrigger

from app.core.database import session_factory
from app.core.unit_of_work import UnitOfWork
from app.infrastructure.onvif import ONVIFCameraClient
from app.application.services.snapshot_service import SnapshotService

scheduler = AsyncIOScheduler()


async def capture_job(camera_id: int) -> None:
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


def schedule_camera(camera_id: int, interval_minutes: int) -> None:
    job_id = f"capture_{camera_id}"
    scheduler.add_job(
        capture_job,
        trigger=IntervalTrigger(minutes=interval_minutes),
        args=[camera_id],
        id=job_id,
        replace_existing=True,
        name=f"Capture {camera_id} every {interval_minutes}m",
    )
    logger.info(f"Scheduled camera {camera_id} every {interval_minutes} min")


def remove_camera_job(camera_id: int) -> None:
    job_id = f"capture_{camera_id}"
    if scheduler.get_job(job_id):
        scheduler.remove_job(job_id)
        logger.info(f"Removed job for camera {camera_id}")


def reschedule_camera(camera_id: int, interval_minutes: int) -> None:
    remove_camera_job(camera_id)
    schedule_camera(camera_id, interval_minutes)


async def load_schedule() -> None:
    async with UnitOfWork(session_factory) as uow:
        cameras = await uow.cameras.get_enabled()
        for cam in cameras:
            schedule_camera(cam.id, cam.interval_minutes)
