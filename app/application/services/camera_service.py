from loguru import logger

from app.core.unit_of_work import UnitOfWork
from app.domain.models import Camera
from app.domain.schemas import CameraCreate, CameraUpdate
from app.infrastructure.onvif import ONVIFCameraClient


class CameraService:
    def __init__(self, uow: UnitOfWork, onvif: ONVIFCameraClient):
        self._uow = uow
        self._onvif = onvif

    async def list_cameras(self) -> list[Camera]:
        cameras = await self._uow.cameras.get_all()
        logger.debug(f"Listing {len(cameras)} cameras")
        return cameras

    async def get_camera(self, camera_id: int) -> Camera | None:
        camera = await self._uow.cameras.get_by_id(camera_id)
        if camera:
            logger.debug(f"Camera {camera_id} ({camera.name}) retrieved")
        else:
            logger.warning(f"Camera {camera_id} not found")
        return camera

    async def create_camera(self, data: CameraCreate) -> Camera:
        camera = Camera(
            name=data.name,
            host=data.host,
            port=data.port,
            username=data.username,
            password=data.password,
            profile_token=data.profile_token,
            interval_minutes=data.interval_minutes,
            enabled=data.enabled,
        )
        await self._uow.cameras.add(camera)
        logger.info(f"Camera created: {camera.name} ({camera.host}) [id={camera.id}]")
        return camera

    async def update_camera(self, camera_id: int, data: CameraUpdate) -> Camera | None:
        values = data.model_dump(exclude_unset=True)
        if not values:
            camera = await self._uow.cameras.get_by_id(camera_id)
            if camera:
                logger.debug(f"No fields to update for camera {camera_id}")
            return camera
        updated = await self._uow.cameras.update(camera_id, values)
        if updated:
            logger.info(f"Camera {camera_id} updated: {set(values.keys())}")
        else:
            logger.warning(f"Camera {camera_id} not found for update")
        return updated

    async def delete_camera(self, camera_id: int) -> bool:
        deleted = await self._uow.cameras.delete(camera_id)
        if deleted:
            logger.info(f"Camera {camera_id} deleted")
        else:
            logger.warning(f"Camera {camera_id} not found for deletion")
        return deleted

    async def test_camera(self, host: str, port: int, username: str, password: str):
        logger.info(f"Testing camera connection to {host}:{port}")
        reachable, profiles, error = self._onvif.test_connection(
            host, port, username, password
        )
        if reachable:
            logger.info(f"Camera {host}:{port} test OK ({len(profiles)} profiles)")
        else:
            logger.warning(f"Camera {host}:{port} test failed: {error}")
        return {
            "reachable": reachable,
            "profiles": profiles,
            "error": error,
        }
