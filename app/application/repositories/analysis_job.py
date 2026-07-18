"""Repository for AnalysisJob persistence.

Provides CRUD operations and queue-management queries for the analysis
job table used by the ML pipeline scheduler.
"""

from datetime import datetime

from sqlalchemy import delete, select, update
from sqlalchemy.ext.asyncio import AsyncSession

from app.domain.models import AnalysisJob


class AnalysisJobRepository:
    """Provide CRUD and query access to AnalysisJob records.

    Args:
        session: The async SQLAlchemy session to use for all operations.
    """

    def __init__(self, session: AsyncSession):
        self._session = session

    async def delete_by_snapshot_ids(self, snapshot_ids: list[int]) -> int:
        """Delete all analysis jobs for the given snapshot IDs.

        Args:
            snapshot_ids: Snapshot primary keys whose jobs should be removed.

        Returns:
            The number of analysis jobs deleted.
        """
        if not snapshot_ids:
            return 0
        result = await self._session.execute(
            delete(AnalysisJob).where(AnalysisJob.snapshot_id.in_(snapshot_ids))
        )
        await self._session.flush()
        return result.rowcount or 0

    async def add(self, job: AnalysisJob) -> None:
        """Persist a new analysis job to the database.

        Args:
            job: The unsaved AnalysisJob instance.
        """
        self._session.add(job)

    async def get_by_id(self, job_id: int) -> AnalysisJob | None:
        """Fetch a single job by its primary key.

        Args:
            job_id: The unique identifier of the job.

        Returns:
            The AnalysisJob if found, otherwise None.
        """
        result = await self._session.execute(
            select(AnalysisJob).where(AnalysisJob.id == job_id)
        )
        return result.scalar_one_or_none()

    async def get_pending(self, limit: int = 10) -> list[AnalysisJob]:
        """Fetch the next batch of pending jobs ordered by priority and age.

        Args:
            limit: Maximum number of jobs to return.

        Returns:
            List of pending AnalysisJob records.
        """
        result = await self._session.execute(
            select(AnalysisJob)
            .where(AnalysisJob.status == "pending")
            .order_by(AnalysisJob.priority.desc(), AnalysisJob.requested_at)
            .limit(limit)
        )
        return list(result.scalars().all())

    async def mark_started(self, job_id: int) -> None:
        """Transition a job from pending to processing and increment attempts.

        Args:
            job_id: The job to mark as started.
        """
        await self._session.execute(
            update(AnalysisJob)
            .where(AnalysisJob.id == job_id)
            .values(status="processing", started_at=datetime.now(), attempts=AnalysisJob.attempts + 1)
        )

    async def mark_completed(self, job_id: int) -> None:
        """Mark a processing job as completed with a finished timestamp.

        Args:
            job_id: The job to mark as completed.
        """
        await self._session.execute(
            update(AnalysisJob)
            .where(AnalysisJob.id == job_id)
            .values(status="completed", finished_at=datetime.now())
        )

    async def mark_failed(self, job_id: int, error_message: str) -> None:
        """Mark a processing job as failed with an error description.

        Args:
            job_id: The job to mark as failed.
            error_message: Human-readable description of what went wrong.
        """
        await self._session.execute(
            update(AnalysisJob)
            .where(AnalysisJob.id == job_id)
            .values(status="failed", error_message=error_message, finished_at=datetime.now())
        )

