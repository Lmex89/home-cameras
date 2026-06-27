from sqlalchemy import select, update, delete
from sqlalchemy.ext.asyncio import AsyncSession

from app.domain.models import Camera


class CameraRepository:
    def __init__(self, session: AsyncSession):
        self._session = session

    async def get_all(self) -> list[Camera]:
        result = await self._session.execute(select(Camera).order_by(Camera.name))
        return list(result.scalars().all())

    async def get_enabled(self) -> list[Camera]:
        result = await self._session.execute(
            select(Camera).where(Camera.enabled == True).order_by(Camera.name)
        )
        return list(result.scalars().all())

    async def get_by_id(self, camera_id: int) -> Camera | None:
        result = await self._session.execute(
            select(Camera).where(Camera.id == camera_id)
        )
        return result.scalar_one_or_none()

    async def add(self, camera: Camera) -> None:
        self._session.add(camera)
        await self._session.flush()

    async def update(self, camera_id: int, values: dict) -> Camera | None:
        result = await self._session.execute(
            update(Camera)
            .where(Camera.id == camera_id)
            .values(**values)
            .returning(Camera)
        )
        await self._session.flush()
        return result.scalar_one_or_none()

    async def delete(self, camera_id: int) -> bool:
        result = await self._session.execute(
            delete(Camera).where(Camera.id == camera_id)
        )
        await self._session.flush()
        return result.rowcount > 0
