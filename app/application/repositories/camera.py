"""Camera repository for persistence of Camera entities.

Exposes a thin data-access layer over an async SQLAlchemy session,
following the standard add/get/get_all repository pattern.
"""

from sqlalchemy import select, update, delete
from sqlalchemy.ext.asyncio import AsyncSession

from app.domain.models import Camera


class CameraRepository:
    """Provide CRUD access to Camera records within a given session.

    Each method flushes pending changes so callers can read generated
    columns (such as ids) immediately after mutation operations.
    """

    def __init__(self, session: AsyncSession):
        """Bind the repository to the supplied async session.

        Args:
            session: The async SQLAlchemy session used for queries.
        """
        self._session = session

    async def get_all(self) -> list[Camera]:
        """Get all cameras ordered by name.

        Returns:
            All Camera rows sorted alphabetically by name.
        """
        result = await self._session.execute(select(Camera).order_by(Camera.name))
        return list(result.scalars().all())

    async def get_enabled(self) -> list[Camera]:
        """Get enabled cameras ordered by name.

        Returns:
            All Camera rows where enabled is True, sorted by name.
        """
        result = await self._session.execute(
            select(Camera).where(Camera.enabled == True).order_by(Camera.name)
        )
        return list(result.scalars().all())

    async def get_by_id(self, camera_id: int) -> Camera | None:
        """Get a single camera by its primary key.

        Args:
            camera_id: The unique identifier of the camera.

        Returns:
            The matching Camera, or None when not found.
        """
        result = await self._session.execute(
            select(Camera).where(Camera.id == camera_id)
        )
        return result.scalar_one_or_none()

    async def add(self, camera: Camera) -> None:
        """Stage a new Camera and flush it to the session.

        Args:
            camera: The Camera instance to persist.
        """
        self._session.add(camera)
        await self._session.flush()

    async def update(self, camera_id: int, values: dict) -> Camera | None:
        """Update columns of an existing Camera by id.

        Args:
            camera_id: The identifier of the camera to update.
            values: Mapping of column names to new values.

        Returns:
            The updated Camera, or None when the id does not exist.
        """
        result = await self._session.execute(
            update(Camera)
            .where(Camera.id == camera_id)
            .values(**values)
            .returning(Camera)
        )
        await self._session.flush()
        return result.scalar_one_or_none()

    async def delete(self, camera_id: int) -> bool:
        """Delete a Camera by id.

        Args:
            camera_id: The identifier of the camera to delete.

        Returns:
            True when a row was deleted, False otherwise.
        """
        result = await self._session.execute(
            delete(Camera).where(Camera.id == camera_id)
        )
        await self._session.flush()
        return result.rowcount > 0