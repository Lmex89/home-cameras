"""Camera management service applying CRUD logic over the UnitOfWork.

Exposes high-level operations (list, get, create, update, delete, test)
above the camera repository and ONVIF client, keeping route handlers free
of business rules.
"""

from loguru import logger

from app.core.unit_of_work import UnitOfWork
from app.domain.models import Camera
from app.domain.schemas import CameraCreate, CameraUpdate
from app.infrastructure.onvif import ONVIFCameraClient


class CameraService:
    """Coordinate camera CRUD operations and ONVIF connectivity tests.

    Receives a ``UnitOfWork`` and an ``ONVIFCameraClient`` via constructor
    injection so the persistence layer and wire protocol stay swappable
    (dependency inversion).
    """

    def __init__(self, uow: UnitOfWork, onvif: ONVIFCameraClient):
        """Inject the unit of work and ONVIF client used by this service.

        Args:
            uow: UnitOfWork wrapping the async SQLAlchemy session.
            onvif: ONVIF camera client used for live connectivity tests.
        """
        self._uow = uow
        self._onvif = onvif

    async def list_cameras(self) -> list[Camera]:
        """Return all cameras currently stored in the database.

        Returns:
            All persisted ``Camera`` instances (may be empty).
        """
        cameras = await self._uow.cameras.get_all()
        logger.debug(f"Listing {len(cameras)} cameras")
        return cameras

    async def get_camera(self, camera_id: int) -> Camera | None:
        """Fetch a single camera by its primary key.

        Args:
            camera_id: Primary key of the camera to retrieve.

        Returns:
            The matching ``Camera`` or ``None`` when not found.
        """
        camera = await self._uow.cameras.get_by_id(camera_id)
        if camera:
            logger.debug(f"Camera {camera_id} ({camera.name}) retrieved")
        else:
            logger.warning(f"Camera {camera_id} not found")
        return camera

    async def create_camera(self, data: CameraCreate) -> Camera:
        """Persist a new camera built from the validated create schema.

        Args:
            data: Create payload with camera connection and capture fields.

        Returns:
            The newly created ``Camera`` with its assigned identifier.
        """
        camera = Camera(
            name=data.name,
            host=data.host,
            port=data.port,
            username=data.username,
            password=data.password,
            profile_token=data.profile_token,
            snapshot_url=data.snapshot_url,
            interval_seconds=data.interval_seconds,
            enabled=data.enabled,
        )
        await self._uow.cameras.add(camera)
        logger.info(f"Camera created: {camera.name} ({camera.host}) [id={camera.id}]")
        return camera

    async def update_camera(self, camera_id: int, data: CameraUpdate) -> Camera | None:
        """Apply a partial update to an existing camera.

        Args:
            camera_id: Primary key of the camera to update.
            data: Update payload; only provided fields are applied.

        Returns:
            The updated ``Camera``, the unchanged camera when no fields were
            set, or ``None`` when the camera does not exist.
        """
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
        """Remove a camera from the database.

        Args:
            camera_id: Primary key of the camera to delete.

        Returns:
            ``True`` when the camera was deleted, ``False`` otherwise.
        """
        deleted = await self._uow.cameras.delete(camera_id)
        if deleted:
            logger.info(f"Camera {camera_id} deleted")
        else:
            logger.warning(f"Camera {camera_id} not found for deletion")
        return deleted

    async def commit(self) -> None:
        """Commit the current unit of work explicitly.

        Allows callers to persist changes before triggering side effects such
        as scheduler updates.
        """
        await self._uow.commit()

    async def test_camera(self, host: str, port: int, username: str, password: str):
        """Probe a camera's ONVIF endpoint and list its media profiles.

        Args:
            host: Camera hostname or IP address.
            port: ONVIF service port.
            username: Credentials username (may be empty).
            password: Credentials password (may be empty).

        Returns:
            Dict with ``reachable`` flag, ``profiles`` list, and ``error``
            message (or ``None`` on success).
        """
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