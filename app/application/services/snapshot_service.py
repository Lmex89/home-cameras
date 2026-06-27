from collections import defaultdict
from datetime import date, datetime, timedelta

import httpx
from loguru import logger
from pathlib import Path

from app.core.config import settings
from app.core.unit_of_work import UnitOfWork
from app.domain.models import Camera, Snapshot
from app.infrastructure.onvif import ONVIFCameraClient


class SnapshotService:
    def __init__(self, uow: UnitOfWork, onvif: ONVIFCameraClient):
        self._uow = uow
        self._onvif = onvif

    async def capture(self, camera: Camera) -> Snapshot:
        now = datetime.now()
        rel_path = Path(str(camera.id)) / now.strftime("%Y/%m/%d")
        filename = f"{now.strftime('%H%M%S')}.jpg"
        full_dir = settings.snapshots_dir / rel_path
        full_dir.mkdir(parents=True, exist_ok=True)
        file_path = full_dir / filename

        snapshot = Snapshot(
            camera_id=camera.id,
            image_path=str(rel_path / filename),
        )

        try:
            uri, error = self._onvif.get_snapshot_uri(
                camera.host, camera.port, camera.username, camera.password, camera.profile_token
            )
            if error or not uri:
                snapshot.status = "error"
                snapshot.error_message = error or "Empty URI"
                logger.warning(f"Camera {camera.name} ({camera.host}): snapshot URI error: {error}")
                await self._uow.snapshots.add(snapshot)
                return snapshot

            auth_uri = self._onvif.build_auth_url(uri, camera.username, camera.password)
            async with httpx.AsyncClient(timeout=30, verify=False) as client:
                resp = await client.get(auth_uri)
                resp.raise_for_status()
                file_path.write_bytes(resp.content)

            snapshot.file_size = file_path.stat().st_size
            snapshot.status = "success"
            logger.info(f"Camera {camera.name} ({camera.host}): snapshot saved to {file_path}")
        except httpx.HTTPStatusError as e:
            snapshot.status = "error"
            snapshot.error_message = str(e)
            logger.warning(f"Camera {camera.name} ({camera.host}): HTTP error fetching snapshot: {e.response.status_code}")
        except Exception as e:
            snapshot.status = "error"
            snapshot.error_message = str(e)
            logger.error(f"Camera {camera.name} ({camera.host}): snapshot capture failed: {e}")

        await self._uow.snapshots.add(snapshot)
        return snapshot

    async def force_capture(self, camera_id: int) -> Snapshot | None:
        camera = await self._uow.cameras.get_by_id(camera_id)
        if not camera:
            logger.warning(f"Force capture: camera {camera_id} not found")
            return None
        logger.info(f"Force capture triggered for camera {camera.name} ({camera.host})")
        return await self.capture(camera)

    async def get_daily_report(self, target_date: date):
        all_snapshots = await self._uow.snapshots.get_by_date(target_date)
        logger.debug(f"Daily report for {target_date}: {len(all_snapshots)} snapshots total")
        grouped: dict[int, list[Snapshot]] = defaultdict(list)
        for snap in all_snapshots:
            grouped[snap.camera_id].append(snap)
        cameras_snapshots = {}
        for camera_id, snap_list in grouped.items():
            cam = await self._uow.cameras.get_by_id(camera_id)
            name = cam.name if cam else f"Camera {camera_id}"
            cameras_snapshots[camera_id] = {"name": name, "snapshots": snap_list}
        return {
            "date": target_date.isoformat(),
            "cameras": cameras_snapshots,
        }

    async def get_camera_snapshots(
        self, camera_id: int, target_date: date
    ) -> list[Snapshot]:
        snapshots = await self._uow.snapshots.get_by_camera_and_date(camera_id, target_date)
        logger.debug(f"Camera {camera_id} snapshots on {target_date}: {len(snapshots)}")
        return snapshots

    async def get_dashboard_data(self):
        from app.domain.schemas import CameraRead, SnapshotRead
        cameras = await self._uow.cameras.get_all()
        camera_ids = [c.id for c in cameras]
        last_snapshots = await self._uow.snapshots.get_last_for_all_cameras(camera_ids)
        result = []
        for cam in cameras:
            cam_read = CameraRead.model_validate(cam)
            last_snap = last_snapshots.get(cam.id)
            snap_read = SnapshotRead.model_validate(last_snap) if last_snap else None
            result.append({
                **cam_read.model_dump(),
                "last_snapshot": snap_read.model_dump() if snap_read else None,
            })
        return result

    async def delete_old_snapshots(self) -> int:
        cutoff = datetime.now() - timedelta(days=settings.snapshot_retention_days)
        deleted = await self._uow.snapshots.delete_older_than(cutoff)
        if deleted:
            logger.info(f"Deleted {deleted} old snapshots (before {cutoff.date()})")
        else:
            logger.debug(f"No old snapshots to delete (before {cutoff.date()})")
        return deleted
