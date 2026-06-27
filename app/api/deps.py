from collections.abc import AsyncGenerator

from fastapi import Depends
from loguru import logger
from sqlalchemy.ext.asyncio import async_sessionmaker

from app.core.database import session_factory
from app.core.unit_of_work import UnitOfWork
from app.application.services.camera_service import CameraService
from app.application.services.snapshot_service import SnapshotService
from app.infrastructure.onvif import ONVIFCameraClient


async def get_uow() -> AsyncGenerator[UnitOfWork, None]:
    async with UnitOfWork(session_factory) as uow:
        logger.debug("UnitOfWork opened")
        yield uow
        logger.debug("UnitOfWork closed")


def get_onvif() -> ONVIFCameraClient:
    return ONVIFCameraClient()


async def get_camera_service(
    uow: UnitOfWork = Depends(get_uow),
    onvif: ONVIFCameraClient = Depends(get_onvif),
) -> AsyncGenerator[CameraService, None]:
    yield CameraService(uow, onvif)


async def get_snapshot_service(
    uow: UnitOfWork = Depends(get_uow),
    onvif: ONVIFCameraClient = Depends(get_onvif),
) -> AsyncGenerator[SnapshotService, None]:
    yield SnapshotService(uow, onvif)
