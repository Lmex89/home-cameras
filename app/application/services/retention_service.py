"""Snapshot and video retention service.

Automates the data lifecycle: raw files → zipped archives → deletion.
Snapshots and videos older than ``zip_after_days`` are compressed into
per-camera/per-day ZIP archives in ``data/archives/``. Once past the
retention period the archives and database records are removed.
"""

import zipfile
from datetime import datetime, timedelta
from pathlib import Path
from zoneinfo import ZoneInfo

from loguru import logger

from app.core.config import settings
from app.core.unit_of_work import UnitOfWork
from app.domain.models import Snapshot


class RetentionService:
    """Manage the snapshot and video data lifecycle.

    Args:
        uow: UnitOfWork wrapping the async SQLAlchemy session.
    """

    def __init__(self, uow: UnitOfWork):
        self._uow = uow

    # ── public API ────────────────────────────────────────────────────

    async def run(self) -> dict:
        """Execute all retention steps in order.

        Returns:
            Summary dict with counts for each operation.
        """
        tz = ZoneInfo(settings.timezone)
        now = datetime.now(tz)
        zip_cutoff = now - timedelta(days=settings.snapshot_zip_after_days)
        delete_cutoff = now - timedelta(days=settings.snapshot_retention_days)
        video_delete_cutoff = now - timedelta(days=settings.video_retention_days)

        result: dict[str, int] = {}

        result["snapshots_zipped"] = await self._zip_old_snapshots(zip_cutoff)
        result["snapshots_deleted"] = await self._delete_expired_snapshots(delete_cutoff)
        result["videos_archived"] = await self._archive_old_videos(zip_cutoff)
        result["videos_deleted"] = await self._delete_expired_videos(video_delete_cutoff)

        total = sum(result.values())
        if total > 0:
            logger.info(f"Retention cleanup complete: {result}")

        return result

    # ── snapshot archiving ────────────────────────────────────────────

    async def _zip_old_snapshots(self, cutoff: datetime) -> int:
        """Compress snapshots older than *cutoff* into daily ZIP archives.

        Snapshot rows are updated with an ``archive_path`` like
        ``snapshots/{camera_id}/{date}.zip::{filename}`` and the original
        JPG file is deleted from ``data/snapshots/``.

        Args:
            cutoff: Exclusive lower-bound timestamp; snapshots older than
                this are zipped and their raw files deleted.

        Returns:
            The number of snapshots successfully archived.
        """
        snapshots = await self._uow.snapshots.get_old_unarchived(cutoff)
        if not snapshots:
            return 0

        groups: dict[tuple[int, str], list[Snapshot]] = {}
        for snap in snapshots:
            snap_date = snap.captured_at.strftime("%Y-%m-%d")
            key = (snap.camera_id, snap_date)
            groups.setdefault(key, []).append(snap)

        archived = 0
        batch_size = 50
        archives_base = settings.archives_dir / "snapshots"
        for (cam_id, date_str), group in groups.items():
            zip_rel = f"snapshots/{cam_id}/{date_str}.zip"
            zip_abs = archives_base / str(cam_id) / f"{date_str}.zip"
            zip_abs.parent.mkdir(parents=True, exist_ok=True)

            with zipfile.ZipFile(zip_abs, "a", zipfile.ZIP_DEFLATED) as zf:
                batch_count = 0
                missing_ids: list[int] = []
                for snap in group:
                    if not snap.image_path or not (settings.snapshots_dir / snap.image_path).exists():
                        missing_ids.append(snap.id)
                        continue
                    src = settings.snapshots_dir / snap.image_path
                    arcname = src.name
                    if arcname not in zf.namelist():
                        zf.write(src, arcname)
                    # Verify file was actually written to the ZIP
                    if arcname not in zf.namelist():
                        logger.error(f"Failed to add {src} to {zip_abs}, skipping deletion")
                        continue
                    archive_ref = f"{zip_rel}::{arcname}"
                    try:
                        await self._uow.snapshots.update_archive_path(snap.id, archive_ref)
                    except Exception:
                        logger.exception(f"Failed to update archive_path for snapshot {snap.id}, skipping deletion")
                        continue
                    src.unlink(missing_ok=True)
                    archived += 1
                    batch_count += 1

                    if batch_count >= batch_size:
                        await self._uow.commit()
                        batch_count = 0
                if batch_count > 0:
                    await self._uow.commit()
                if missing_ids:
                    marked = await self._uow.snapshots.mark_archived_batch(
                        missing_ids, f"{zip_rel}::<missing>"
                    )
                    await self._uow.commit()
                    logger.warning(
                        f"Marked {marked} snapshot(s) as <missing> for "
                        f"camera {cam_id} on {date_str} (raw file absent)"
                    )

        logger.info(f"Archived {archived} snapshots into {len(groups)} ZIP files")
        return archived

    async def _delete_expired_snapshots(self, cutoff: datetime) -> int:
        """Delete snapshot records and their archive ZIPs past the retention cutoff.

        A ZIP file is only removed once *all* snapshots referencing it have
        been deleted (i.e. are also past the cutoff).

        Args:
            cutoff: Exclusive lower-bound timestamp; snapshots older than
                this are deleted from the database and their archive files
                removed from disk.

        Returns:
            The number of database records deleted.
        """
        deleted = await self._uow.snapshots.delete_older_than(cutoff)

        archives_base = settings.archives_dir / "snapshots"
        if archives_base.exists():
            for zip_path in archives_base.rglob("*.zip"):
                rel = zip_path.relative_to(settings.archives_dir)
                count = await self._uow.snapshots.count_by_archive_zip(str(rel))
                if count == 0:
                    zip_path.unlink(missing_ok=True)
                    logger.debug(f"Deleted orphaned archive: {zip_path}")

        if deleted:
            logger.info(f"Deleted {deleted} expired snapshot records")
        return deleted

    # ── video archiving ───────────────────────────────────────────────

    async def _archive_old_videos(self, cutoff: datetime) -> int:
        """Compress videos older than *cutoff* into daily ZIP archives.

        Videos are grouped by camera id (from filename) and date, then
        packed into ``data/archives/videos/{camera_id}/{date}.zip``. The
        original MP4 is deleted after archiving.

        Args:
            cutoff: Exclusive lower-bound timestamp; videos older than
                this are zipped and the raw MP4 deleted.

        Returns:
            The number of videos archived.
        """
        videos_dir = settings.videos_dir
        if not videos_dir.exists():
            return 0

        archived = 0
        archives_base = settings.archives_dir / "videos"
        groups: dict[tuple[str, str], list[Path]] = {}

        for mp4 in videos_dir.glob("*.mp4"):
            mtime = datetime.fromtimestamp(mp4.stat().st_mtime, tz=ZoneInfo(settings.timezone))
            if mtime >= cutoff:
                continue
            # Parse camera id and date from filename e.g.
            # timelapse_3_2025-06-15_h14.mp4
            parts = mp4.stem.split("_")
            cam_id = parts[1] if len(parts) > 1 else "0"
            date_part = parts[2] if len(parts) > 2 else mtime.strftime("%Y-%m-%d")
            key = (cam_id, date_part)
            groups.setdefault(key, []).append(mp4)

        for (cam_id, date_str), files in groups.items():
            zip_abs = archives_base / cam_id / f"{date_str}.zip"
            zip_abs.parent.mkdir(parents=True, exist_ok=True)
            with zipfile.ZipFile(zip_abs, "a", zipfile.ZIP_DEFLATED) as zf:
                for mp4 in files:
                    if mp4.name not in zf.namelist():
                        zf.write(mp4, mp4.name)
                    mp4.unlink(missing_ok=True)
                    archived += 1

        if archived:
            logger.info(f"Archived {archived} videos into ZIP archives")
        return archived

    async def _delete_expired_videos(self, cutoff: datetime) -> int:
        """Delete video archive ZIPs and any stray MP4 files past retention.

        Args:
            cutoff: Exclusive lower-bound timestamp; files older than
                this are removed from disk.

        Returns:
            The number of files deleted.
        """
        deleted = 0
        archives_base = settings.archives_dir / "videos"
        if archives_base.exists():
            for zip_path in archives_base.rglob("*.zip"):
                mtime = datetime.fromtimestamp(zip_path.stat().st_mtime, tz=ZoneInfo(settings.timezone))
                if mtime < cutoff:
                    zip_path.unlink()
                    deleted += 1

        # Also delete any stray raw MP4 files past retention
        videos_dir = settings.videos_dir
        if videos_dir.exists():
            for mp4 in videos_dir.glob("*.mp4"):
                mtime = datetime.fromtimestamp(mp4.stat().st_mtime, tz=ZoneInfo(settings.timezone))
                if mtime < cutoff:
                    mp4.unlink(missing_ok=True)
                    deleted += 1

        if deleted:
            logger.info(f"Deleted {deleted} expired video files")
        return deleted
