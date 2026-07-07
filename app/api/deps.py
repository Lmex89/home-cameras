"""FastAPI dependency providers for the cameras application.

Exposes dependency-injection callables that construct the Unit of Work, the
ONVIF camera client, and the camera, snapshot, and analysis services for
use in route functions via ``Depends``.
"""

from collections.abc import AsyncGenerator

from fastapi import Depends
from loguru import logger

from app.core.database import session_factory
from app.core.unit_of_work import UnitOfWork
from app.application.services.camera_service import CameraService
from app.application.services.snapshot_service import SnapshotService
from app.application.services.analysis_service import AnalysisService
from app.infrastructure.onvif import ONVIFCameraClient


async def get_uow() -> AsyncGenerator[UnitOfWork, None]:
    """Provide a UnitOfWork as a FastAPI dependency.

    Yields:
        A UnitOfWork instance backed by the shared session factory.
    """
    async with UnitOfWork(session_factory) as uow:
        logger.debug("UnitOfWork opened")
        yield uow
        logger.debug("UnitOfWork closed")


def get_onvif() -> ONVIFCameraClient:
    """Provide an ONVIF camera client instance.

    Returns:
        A new ONVIFCameraClient instance.
    """
    return ONVIFCameraClient()


async def get_camera_service(
    uow: UnitOfWork = Depends(get_uow),
    onvif: ONVIFCameraClient = Depends(get_onvif),
) -> AsyncGenerator[CameraService, None]:
    """Provide a CameraService wired with its dependencies.

    Args:
        uow: The Unit of Work for database access.
        onvif: The ONVIF camera client for device communication.

    Yields:
        A CameraService instance ready for use in route handlers.
    """
    yield CameraService(uow, onvif)


async def get_snapshot_service(
    uow: UnitOfWork = Depends(get_uow),
    onvif: ONVIFCameraClient = Depends(get_onvif),
) -> AsyncGenerator[SnapshotService, None]:
    """Provide a SnapshotService wired with its dependencies.

    Args:
        uow: The Unit of Work for database access.
        onvif: The ONVIF camera client for device communication.

    Yields:
        A SnapshotService instance ready for use in route handlers.
    """
    yield SnapshotService(uow, onvif)


async def get_analysis_service(
    uow: UnitOfWork = Depends(get_uow),
) -> AsyncGenerator[AnalysisService, None]:
    """Provide an AnalysisService wired with its dependencies.

    Args:
        uow: The Unit of Work for database access.

    Yields:
        An AnalysisService instance ready for use in route handlers.
    """
    yield AnalysisService(uow)
