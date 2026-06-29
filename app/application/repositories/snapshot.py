"""Snapshot repository for persistence of Snapshot entities.

Provides date-ordered queries, latest-by-camera lookups, and retention
cleanup over an async SQLAlchemy session.
"""

from datetime import date, datetime

from sqlalchemy import select, func, delete, update
from sqlalchemy.ext.asyncio import AsyncSession

from app.domain.models import Snapshot


class SnapshotRepository:
    """Provide query and mutation access to Snapshot records.

    All mutation methods flush the session so callers can rely on
    generated columns and rowcount metadata immediately afterwards.
    """

    def __init__(self, session: AsyncSession):
        """Bind the repository to the supplied async session.

        Args:
            session: The async SQLAlchemy session used for queries.
        """
        self._session = session

    async def get_by_id(self, snapshot_id: int) -> Snapshot | None:
        """Get a single snapshot by its primary key.

        Args:
            snapshot_id: The unique identifier of the snapshot.

        Returns:
            The matching Snapshot, or None when not found.
        """
        result = await self._session.execute(
            select(Snapshot).where(Snapshot.id == snapshot_id)
        )
        return result.scalar_one_or_none()

    async def get_by_camera_and_date(
        self, camera_id: int, target_date: date
    ) -> list[Snapshot]:
        """Get all snapshots for a camera captured on a given date.

        Args:
            camera_id: The identifier of the camera.
            target_date: The calendar date to query (full day range).

        Returns:
            Snapshots ordered by captured_at timestamp.
        """
        start = datetime.combine(target_date, datetime.min.time())
        end = datetime.combine(target_date, datetime.max.time())
        result = await self._session.execute(
            select(Snapshot)
            .where(
                Snapshot.camera_id == camera_id,
                Snapshot.captured_at >= start,
                Snapshot.captured_at <= end,
            )
            .order_by(Snapshot.captured_at)
        )
        return list(result.scalars().all())

    async def get_by_date(self, target_date: date) -> list[Snapshot]:
        """Get all snapshots captured on a given date across cameras.

        Args:
            target_date: The calendar date to query (full day range).

        Returns:
            Snapshots ordered by camera_id then captured_at timestamp.
        """
        start = datetime.combine(target_date, datetime.min.time())
        end = datetime.combine(target_date, datetime.max.time())
        result = await self._session.execute(
            select(Snapshot)
            .where(Snapshot.captured_at >= start, Snapshot.captured_at <= end)
            .order_by(Snapshot.camera_id, Snapshot.captured_at)
        )
        return list(result.scalars().all())

    async def get_last_by_camera(self, camera_id: int) -> Snapshot | None:
        """Get the most recent successful snapshot for a camera.

        Args:
            camera_id: The identifier of the camera.

        Returns:
            The latest successful Snapshot, or None when none exist.
        """
        result = await self._session.execute(
            select(Snapshot)
            .where(Snapshot.camera_id == camera_id, Snapshot.status == "success")
            .order_by(Snapshot.captured_at.desc())
            .limit(1)
        )
        return result.scalar_one_or_none()

    async def get_last_for_all_cameras(
        self, camera_ids: list[int]
    ) -> dict[int, Snapshot | None]:
        """Get the latest successful snapshot for each given camera.

        Args:
            camera_ids: List of camera identifiers to look up.

        Returns:
            Dict keyed by camera_id with the latest successful Snapshot
            or None when a camera has no successful snapshots.
        """
        if not camera_ids:
            return {}
        result = await self._session.execute(
            select(Snapshot)
            .where(
                Snapshot.camera_id.in_(camera_ids),
                Snapshot.status == "success",
            )
            .order_by(Snapshot.captured_at.desc())
        )
        rows = result.scalars().all()
        seen: set[int] = set()
        latest: dict[int, Snapshot | None] = {cid: None for cid in camera_ids}
        for row in rows:
            if row.camera_id not in seen:
                seen.add(row.camera_id)
                latest[row.camera_id] = row
        return latest

    async def add(self, snapshot: Snapshot) -> None:
        """Stage a new Snapshot and flush it to the session.

        Args:
            snapshot: The Snapshot instance to persist.
        """
        self._session.add(snapshot)
        await self._session.flush()

    async def get_all_successful_with_images(self) -> list[Snapshot]:
        """Get all successful snapshots that have an image file saved.

        Used by the live manifest endpoint to build the per-date
        snapshot index without a separate export step.

        Returns:
            Snapshots ordered by camera_id then captured_at timestamp.
        """
        result = await self._session.execute(
            select(Snapshot)
            .where(
                Snapshot.status == "success",
                Snapshot.image_path != "",
            )
            .order_by(Snapshot.camera_id, Snapshot.captured_at)
        )
        return list(result.scalars().all())

    async def count_by_camera_and_date(
        self, camera_id: int, target_date: date
    ) -> int:
        """Count snapshots for a camera on a given date.

        Args:
            camera_id: The identifier of the camera.
            target_date: The calendar date to query (full day range).

        Returns:
            The number of matching snapshots, or 0 when none exist.
        """
        start = datetime.combine(target_date, datetime.min.time())
        end = datetime.combine(target_date, datetime.max.time())
        result = await self._session.execute(
            select(func.count(Snapshot.id)).where(
                Snapshot.camera_id == camera_id,
                Snapshot.captured_at >= start,
                Snapshot.captured_at <= end,
            )
        )
        return result.scalar() or 0

    async def delete_older_than(self, cutoff: datetime) -> int:
        """Delete snapshots captured before a cutoff timestamp.

        Args:
            cutoff: The exclusive lower-bound timestamp; older rows are removed.

        Returns:
            The number of snapshots deleted.
        """
        result = await self._session.execute(
            delete(Snapshot).where(Snapshot.captured_at < cutoff)
        )
        await self._session.flush()
        return result.rowcount

    async def get_old_unarchived(self, cutoff: datetime) -> list[Snapshot]:
        """Get snapshots older than cutoff that haven't been archived yet.

        Args:
            cutoff: The exclusive lower-bound timestamp.

        Returns:
            Snapshots ordered by camera_id then captured_at.
        """
        result = await self._session.execute(
            select(Snapshot)
            .where(Snapshot.captured_at < cutoff, Snapshot.archive_path.is_(None))
            .order_by(Snapshot.camera_id, Snapshot.captured_at)
        )
        return list(result.scalars().all())

    async def update_archive_path(self, snapshot_id: int, archive_path: str) -> None:
        """Set the archive_path for a snapshot.

        Args:
            snapshot_id: The identifier of the snapshot.
            archive_path: The archive path to store (e.g. ``snapshots/3/2025-06-15.zip::150322.jpg``).
        """
        await self._session.execute(
            update(Snapshot)
            .where(Snapshot.id == snapshot_id)
            .values(archive_path=archive_path)
        )
        await self._session.flush()

    async def count_by_archive_zip(self, zip_path: str) -> int:
        """Count snapshots referencing a given archive zip.

        Args:
            zip_path: The archive zip path to match (e.g. ``snapshots/3/2025-06-15.zip``).

        Returns:
            The number of snapshots that reference this zip.
        """
        result = await self._session.execute(
            select(func.count(Snapshot.id)).where(
                Snapshot.archive_path.startswith(zip_path)
            )
        )
        return result.scalar() or 0