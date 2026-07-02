"""Unit of Work pattern wrapping an async SQLAlchemy session.

Defines ``UnitOfWork``, an async context manager that owns a single database
session and exposes the camera, snapshot, analysis job, and snapshot analysis
repositories, committing on success and rolling back on error.
"""

from collections.abc import AsyncGenerator

from loguru import logger
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.application.repositories.camera import CameraRepository
from app.application.repositories.snapshot import SnapshotRepository
from app.application.repositories.analysis_job import AnalysisJobRepository
from app.application.repositories.snapshot_analysis import SnapshotAnalysisRepository


class UnitOfWork:
    """Transactional scope owning a database session and repositories.

    On entering the async context a new session is created and all
    repositories are bound to it. On exit the session is committed when no
    exception occurred, otherwise it is rolled back and closed.
    """

    def __init__(self, factory: async_sessionmaker[AsyncSession]):
        self._factory = factory
        self._session: AsyncSession | None = None
        self.cameras: CameraRepository | None = None
        self.snapshots: SnapshotRepository | None = None
        self.analysis_jobs: AnalysisJobRepository | None = None
        self.snapshot_analyses: SnapshotAnalysisRepository | None = None

    async def __aenter__(self) -> "UnitOfWork":
        """Open a new session and initialize the repositories.

        Returns:
            This UnitOfWork instance with an active session.
        """
        self._session = self._factory()
        self.cameras = CameraRepository(self._session)
        self.snapshots = SnapshotRepository(self._session)
        self.analysis_jobs = AnalysisJobRepository(self._session)
        self.snapshot_analyses = SnapshotAnalysisRepository(self._session)
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb) -> None:
        """Commit or roll back the transaction and close the session.

        Args:
            exc_type: The exception class raised within the block, if any.
            exc_val: The exception instance raised, if any.
            exc_tb: The traceback associated with the exception, if any.
        """
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
            self.analysis_jobs = None
            self.snapshot_analyses = None

    async def commit(self) -> None:
        """Commit the current transaction if a session is active."""
        if self._session is not None:
            await self._session.commit()


async def get_uow(
    factory: async_sessionmaker[AsyncSession],
) -> AsyncGenerator[UnitOfWork, None]:
    """Yield a UnitOfWork scoped to the given session factory.

    Args:
        factory: The async session maker used to create the session.

    Yields:
        A UnitOfWork instance wrapping a fresh session.
    """
    async with UnitOfWork(factory) as uow:
        yield uow
