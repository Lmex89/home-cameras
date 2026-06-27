from collections.abc import AsyncGenerator

from loguru import logger
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.application.repositories.camera import CameraRepository
from app.application.repositories.snapshot import SnapshotRepository


class UnitOfWork:
    def __init__(self, factory: async_sessionmaker[AsyncSession]):
        self._factory = factory
        self._session: AsyncSession | None = None
        self.cameras: CameraRepository | None = None
        self.snapshots: SnapshotRepository | None = None

    async def __aenter__(self) -> "UnitOfWork":
        self._session = self._factory()
        self.cameras = CameraRepository(self._session)
        self.snapshots = SnapshotRepository(self._session)
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb) -> None:
        if self._session is None:
            return
        try:
            if exc_type is None:
                await self._session.commit()
            else:
                logger.warning(f"Rolling back transaction due to {exc_type.__name__}")
                await self._session.rollback()
        finally:
            await self._session.close()
            self._session = None
            self.cameras = None
            self.snapshots = None

    async def commit(self) -> None:
        if self._session is not None:
            await self._session.commit()


async def get_uow(
    factory: async_sessionmaker[AsyncSession],
) -> AsyncGenerator[UnitOfWork, None]:
    async with UnitOfWork(factory) as uow:
        yield uow
