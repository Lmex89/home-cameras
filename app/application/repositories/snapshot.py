from datetime import date, datetime

from sqlalchemy import select, func, delete
from sqlalchemy.ext.asyncio import AsyncSession

from app.domain.models import Snapshot


class SnapshotRepository:
    def __init__(self, session: AsyncSession):
        self._session = session

    async def get_by_id(self, snapshot_id: int) -> Snapshot | None:
        result = await self._session.execute(
            select(Snapshot).where(Snapshot.id == snapshot_id)
        )
        return result.scalar_one_or_none()

    async def get_by_camera_and_date(
        self, camera_id: int, target_date: date
    ) -> list[Snapshot]:
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
        start = datetime.combine(target_date, datetime.min.time())
        end = datetime.combine(target_date, datetime.max.time())
        result = await self._session.execute(
            select(Snapshot)
            .where(Snapshot.captured_at >= start, Snapshot.captured_at <= end)
            .order_by(Snapshot.camera_id, Snapshot.captured_at)
        )
        return list(result.scalars().all())

    async def get_last_by_camera(self, camera_id: int) -> Snapshot | None:
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
        self._session.add(snapshot)
        await self._session.flush()

    async def count_by_camera_and_date(
        self, camera_id: int, target_date: date
    ) -> int:
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
        result = await self._session.execute(
            delete(Snapshot).where(Snapshot.captured_at < cutoff)
        )
        await self._session.flush()
        return result.rowcount
