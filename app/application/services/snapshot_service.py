import asyncio
import asyncio
from collections import defaultdict
from datetime import date, datetime
from pathlib import Path

import httpx
from loguru import logger

from app.core.config import settings
from app.core.unit_of_work import UnitOfWork
from app.domain.models import Camera, Snapshot
from app.infrastructure.onvif import ONVIFCameraClient


class SnapshotService:
    def __init__(self, uow: UnitOfWork, onvif: ONVIFCameraClient):
        self._uow = uow
        self._onvif = onvif

    async def _save_image(self, camera: Camera, data: bytes) -> Path:
        now = datetime.now()
        rel_path = Path(str(camera.id)) / now.strftime("%Y/%m/%d")
        filename = f"{now.strftime('%H%M%S')}.jpg"
        full_dir = settings.snapshots_dir / rel_path
        full_dir.mkdir(parents=True, exist_ok=True)
        file_path = full_dir / filename
        file_path.write_bytes(data)
        return file_path

    async def _capture_direct_url(self, camera: Camera) -> tuple[Path | None, str | None]:
        url = camera.snapshot_url
        if not url:
            return None, None
        logger.info(f"Camera {camera.name}: trying direct URL {url}")
        try:
            auth = (camera.username, camera.password) if camera.username else None
            async with httpx.AsyncClient(timeout=30, verify=False) as client:
                resp = await client.get(url, auth=auth)
                resp.raise_for_status()
            file_path = await self._save_image(camera, resp.content)
            return file_path, None
        except Exception as e:
            msg = str(e)
            logger.warning(f"Camera {camera.name}: direct URL failed: {msg}")
            return None, msg

    async def _capture_onvif(self, camera: Camera) -> tuple[Path | None, str | None]:
        logger.info(f"Camera {camera.name}: trying ONVIF GetSnapshotUri")
        try:
            uri, error = self._onvif.get_snapshot_uri(
                camera.host, camera.port, camera.username, camera.password, camera.profile_token
            )
            if error or not uri:
                return None, error or "Empty URI"
            auth_uri = self._onvif.build_auth_url(uri, camera.username, camera.password)
            async with httpx.AsyncClient(timeout=30, verify=False) as client:
                resp = await client.get(auth_uri)
                resp.raise_for_status()
            file_path = await self._save_image(camera, resp.content)
            return file_path, None
        except Exception as e:
            return None, str(e)

    async def _capture_rtsp(self, camera: Camera) -> tuple[Path | None, str | None]:
        logger.info(f"Camera {camera.name}: trying RTSP+ffmpeg")
        try:
            # Strategy: specific profile → best resolution → JPEG → first available
            stream_uri = None
            if camera.profile_token:
                stream_uri, err = self._onvif.get_stream_uri(
                    camera.host, camera.port, camera.username, camera.password, camera.profile_token
                )
            if not stream_uri:
                stream_uri, _, err = self._onvif.get_best_stream_uri(
                    camera.host, camera.port, camera.username, camera.password
                )
            if not stream_uri:
                stream_uri, _, err = self._onvif.get_jpeg_stream_uri(
                    camera.host, camera.port, camera.username, camera.password
                )
            if not stream_uri:
                stream_uri, err = self._onvif.get_first_stream_uri(
                    camera.host, camera.port, camera.username, camera.password
                )
            if not stream_uri:
                return None, err or "No RTSP stream found"

            auth_uri = self._onvif.build_auth_url(stream_uri, camera.username, camera.password)

            proc = await asyncio.create_subprocess_exec(
                'ffmpeg',
                '-rtsp_transport', 'tcp',
                '-i', auth_uri,
                '-vframes', '1',
                '-f', 'image2pipe',
                '-vcodec', 'mjpeg',
                '-',
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await proc.communicate()
            if proc.returncode != 0 or not stdout:
                error_msg = (stderr.decode(errors='replace')[:200] if stderr else "ffmpeg returned no data")
                return None, f"ffmpeg failed: {error_msg}"

            file_path = await self._save_image(camera, stdout)
            return file_path, None
        except FileNotFoundError:
            return None, "ffmpeg not found in PATH"
        except Exception as e:
            return None, str(e)

    async def capture(self, camera: Camera) -> Snapshot:
        snapshot = Snapshot(
            camera_id=camera.id,
            image_path="",
        )

        # Strategy 1: direct URL
        if camera.snapshot_url:
            file_path, error = await self._capture_direct_url(camera)
            if file_path:
                snapshot.image_path = str(file_path.relative_to(settings.snapshots_dir))
                snapshot.file_size = file_path.stat().st_size
                snapshot.status = "success"
                logger.info(f"Camera {camera.name}: direct URL snapshot saved")
                await self._uow.snapshots.add(snapshot)
                return snapshot
            snapshot.error_message = error

        # Strategy 2: ONVIF GetSnapshotUri
        file_path, error = await self._capture_onvif(camera)
        if file_path:
            snapshot.image_path = str(file_path.relative_to(settings.snapshots_dir))
            snapshot.file_size = file_path.stat().st_size
            snapshot.status = "success"
            logger.info(f"Camera {camera.name}: ONVIF snapshot saved to {file_path}")
            await self._uow.snapshots.add(snapshot)
            return snapshot

        # Strategy 3: RTSP+ffmpeg fallback
        file_path, error = await self._capture_rtsp(camera)
        if file_path:
            snapshot.image_path = str(file_path.relative_to(settings.snapshots_dir))
            snapshot.file_size = file_path.stat().st_size
            snapshot.status = "success"
            logger.info(f"Camera {camera.name}: RTSP snapshot saved to {file_path}")
            await self._uow.snapshots.add(snapshot)
            return snapshot

        snapshot.status = "error"
        snapshot.error_message = error or "All capture methods failed"
        logger.error(f"Camera {camera.name}: all capture methods failed: {snapshot.error_message}")
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


