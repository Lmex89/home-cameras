"""Repository for SnapshotAnalysis persistence.

Provides CRUD operations and review-queue queries for the ML analysis
results stored in the ``snapshot_analyses`` table.
"""

from datetime import datetime

from sqlalchemy import delete, func, select, update
from sqlalchemy.ext.asyncio import AsyncSession

from app.domain.models import SnapshotAnalysis


class SnapshotAnalysisRepository:
    """Provide CRUD and query access to SnapshotAnalysis records.

    Args:
        session: The async SQLAlchemy session to use for all operations.
    """

    def __init__(self, session: AsyncSession):
        self._session = session

    async def delete_by_snapshot_ids(self, snapshot_ids: list[int]) -> int:
        """Delete all analysis records for the given snapshot IDs.

        Args:
            snapshot_ids: Snapshot primary keys whose analyses should be removed.

        Returns:
            The number of analysis records deleted.
        """
        if not snapshot_ids:
            return 0
        result = await self._session.execute(
            delete(SnapshotAnalysis).where(SnapshotAnalysis.snapshot_id.in_(snapshot_ids))
        )
        await self._session.flush()
        return result.rowcount or 0

    async def add(self, analysis: SnapshotAnalysis) -> None:
        """Persist a new analysis result to the database.

        Args:
            analysis: The unsaved SnapshotAnalysis instance.
        """
        self._session.add(analysis)

    async def get_by_id(self, analysis_id: int) -> SnapshotAnalysis | None:
        """Fetch a single analysis record by its primary key.

        Args:
            analysis_id: The unique identifier of the analysis.

        Returns:
            The SnapshotAnalysis if found, otherwise None.
        """
        result = await self._session.execute(
            select(SnapshotAnalysis).where(SnapshotAnalysis.id == analysis_id)
        )
        return result.scalar_one_or_none()

    async def get_by_snapshot(self, snapshot_id: int, model_name: str | None = None) -> list[SnapshotAnalysis]:
        """Find all analyses for a given snapshot, optionally filtered by model.

        Args:
            snapshot_id: The snapshot to query analyses for.
            model_name: Optional model name filter (e.g. ``yolov8n``).

        Returns:
            List of matching SnapshotAnalysis records (may be empty).
        """
        stmt = select(SnapshotAnalysis).where(SnapshotAnalysis.snapshot_id == snapshot_id)
        if model_name:
            stmt = stmt.where(SnapshotAnalysis.model_name == model_name)
        result = await self._session.execute(stmt)
        return list(result.scalars().all())

    async def get_pending_reviews(self, limit: int = 5000) -> list[SnapshotAnalysis]:
        """Fetch analyses that are flagged for human review.

        Args:
            limit: Maximum number of items to return.

        Returns:
            Ordered list of SnapshotAnalysis records requiring review.
        """
        result = await self._session.execute(
            select(SnapshotAnalysis)
            .where(SnapshotAnalysis.review_required == True)

            .order_by(SnapshotAnalysis.analyzed_at.desc())
            .limit(limit)
        )
        return list(result.scalars().all())

    async def get_by_camera_and_date(
        self, camera_id: int, snapshot_ids: list[int]
    ) -> list[SnapshotAnalysis]:
        """Fetch analyses for a set of snapshot IDs belonging to one camera.

        Used by the report endpoint to enrich snapshots with analysis data.

        Args:
            camera_id: The camera these snapshots belong to (used for filtering).
            snapshot_ids: List of snapshot primary keys to query.

        Returns:
            List of matching SnapshotAnalysis records ordered by most recent.
        """
        if not snapshot_ids:
            return []
        result = await self._session.execute(
            select(SnapshotAnalysis)
            .where(SnapshotAnalysis.snapshot_id.in_(snapshot_ids))
            .order_by(SnapshotAnalysis.analyzed_at.desc())
        )
        return list(result.scalars().all())

    async def count_pending_reviews(self) -> int:
        """Return the number of analyses flagged for human review.

        Returns:
            Total count of flagged but un-reviewed analyses.
        """
        result = await self._session.execute(
            select(func.count(SnapshotAnalysis.id)).where(
                SnapshotAnalysis.review_required == True,

            )
        )
        return result.scalar() or 0

    async def update_review(
        self, analysis_id: int, review_required: bool, review_reason: str | None = None
    ) -> None:
        """Update the review flag and optional reason for an analysis.

        Args:
            analysis_id: The analysis to update.
            review_required: Whether the snapshot still requires review.
            review_reason: Optional explanation for the review decision.
        """
        values: dict = {
            "review_required": review_required,
            "updated_at": datetime.now(),
        }
        if review_reason is not None:
            values["review_reason"] = review_reason
        await self._session.execute(
            update(SnapshotAnalysis).where(SnapshotAnalysis.id == analysis_id).values(**values)
        )

    async def get_detections(
        self,
        days_back: int = 1,
        camera_id: int | None = None,
        class_name: str | None = None,
        limit: int = 500,
        offset: int = 0,
        date_from: str | None = None,
    ) -> list[tuple]:
        """Fetch all analyses with detections, joined with snapshot and camera data.

        Args:
            days_back: Only include analyses from the last N days.
            camera_id: Optional camera ID filter.
            class_name: Optional object class filter (JSON text search).
            limit: Maximum rows to return.
            offset: Pagination offset.
            date_from: Specific date (YYYY-MM-DD) — overrides days_back.

        Returns:
            List of tuples with (analysis fields, camera name, snapshot fields).
        """
        from datetime import datetime, timedelta

        if date_from:
            since = datetime.strptime(date_from, "%Y-%m-%d")
            until = since + timedelta(days=1)
        else:
            now = datetime.now()
            if days_back == 0:
                since = now.replace(hour=0, minute=0, second=0, microsecond=0)
            else:
                since = now - timedelta(days=days_back)
            until = None
        from app.domain.models import Snapshot, Camera

        stmt = (
            select(
                SnapshotAnalysis,
                Camera.name,
                Snapshot.captured_at,
                Snapshot.image_path,
                Snapshot.camera_id,
            )
            .join(Snapshot, SnapshotAnalysis.snapshot_id == Snapshot.id)
            .join(Camera, Snapshot.camera_id == Camera.id)
            .where(
                SnapshotAnalysis.objects_json.isnot(None),
                SnapshotAnalysis.analyzed_at >= since,
            )
            .order_by(SnapshotAnalysis.analyzed_at.desc())
            .offset(offset)
            .limit(limit)
        )
        if until is not None:
            stmt = stmt.where(SnapshotAnalysis.analyzed_at < until)
        if camera_id is not None:
            stmt = stmt.where(Snapshot.camera_id == camera_id)
        if class_name is not None:
            stmt = stmt.where(SnapshotAnalysis.objects_json.contains(class_name))

        result = await self._session.execute(stmt)
        return list(result.all())

