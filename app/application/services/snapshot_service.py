"""Snapshot capture and reporting service.

Implements the multi-strategy snapshot pipeline (direct URL, ONVIF
``GetSnapshotUri``, RTSP+ffmpeg fallback), persists snapshots via the
UnitOfWork, and produces daily reports, dashboard data, and timelapse
videos.
"""

import asyncio
import shutil
import tempfile
from collections import defaultdict
from datetime import date, datetime
from pathlib import Path
from zoneinfo import ZoneInfo

import httpx
from loguru import logger

from app.core.config import settings
from app.core.unit_of_work import UnitOfWork
from app.domain.models import Camera, Snapshot
from app.infrastructure.onvif import ONVIFCameraClient


class SnapshotService:
    """Capture snapshots from cameras and provide reporting utilities.

    Receives a ``UnitOfWork`` and an ``ONVIFCameraClient`` via constructor
    injection so persistence and the ONVIF wire protocol stay decoupled
    (dependency inversion).
    """

    def __init__(self, uow: UnitOfWork, onvif: ONVIFCameraClient):
        """Inject the unit of work and ONVIF client used by this service.

        Args:
            uow: UnitOfWork wrapping the async SQLAlchemy session.
            onvif: ONVIF camera client used for URI resolution.
        """
        self._uow = uow
        self._onvif = onvif

    async def _save_image(self, camera: Camera, data: bytes) -> tuple[Path, datetime]:
        """Write raw image bytes to disk under the snapshots directory.

        Args:
            camera: The camera the image belongs to.
            data: Raw JPEG bytes to persist.

        Returns:
            Tuple of the absolute file path and capture timestamp.
        """
        now = datetime.now(ZoneInfo(settings.timezone))
        rel_path = Path(str(camera.id)) / now.strftime("%Y/%m/%d")
        filename = f"{now.strftime('%H%M%S')}.jpg"
        full_dir = settings.snapshots_dir / rel_path
        full_dir.mkdir(parents=True, exist_ok=True)
        file_path = full_dir / filename
        file_path.write_bytes(data)
        return file_path, now

    async def _capture_direct_url(self, camera: Camera) -> tuple[Path | None, datetime | None, str | None]:
        """Fetch a snapshot from the camera's configured direct URL.

        Args:
            camera: Camera whose ``snapshot_url`` will be requested.

        Returns:
            Tuple of (file path, capture time, error). On failure the path
            and time are ``None`` and the error string is populated.
        """
        url = camera.snapshot_url
        if not url:
            return None, None, None
        logger.info(f"Camera {camera.name}: trying direct URL {url}")
        try:
            auth = (camera.username, camera.password) if camera.username else None
            async with httpx.AsyncClient(timeout=30, verify=False) as client:
                resp = await client.get(url, auth=auth)
                resp.raise_for_status()
            file_path, captured_at = await self._save_image(camera, resp.content)
            return file_path, captured_at, None
        except Exception as e:
            msg = str(e)
            logger.warning(f"Camera {camera.name}: direct URL failed: {msg}")
            return None, None, msg

    async def _capture_onvif(self, camera: Camera) -> tuple[Path | None, datetime | None, str | None]:
        """Fetch a snapshot via ONVIF ``GetSnapshotUri``.

        Args:
            camera: Camera used to resolve the snapshot URI.

        Returns:
            Tuple of (file path, capture time, error). On failure the path
            and time are ``None`` and the error string is populated.
        """
        logger.info(f"Camera {camera.name}: trying ONVIF GetSnapshotUri")
        try:
            uri, error = self._onvif.get_snapshot_uri(
                camera.host, camera.port, camera.username, camera.password, camera.profile_token
            )
            if error or not uri:
                return None, None, error or "Empty URI"
            auth_uri = self._onvif.build_auth_url(uri, camera.username, camera.password)
            async with httpx.AsyncClient(timeout=30, verify=False) as client:
                resp = await client.get(auth_uri)
                resp.raise_for_status()
            file_path, captured_at = await self._save_image(camera, resp.content)
            return file_path, captured_at, None
        except Exception as e:
            return None, None, str(e)

    async def _capture_rtsp(self, camera: Camera) -> tuple[Path | None, datetime | None, str | None]:
        """Grab a single frame from the RTSP stream using ffmpeg.

        Resolution strategy mirrors the inline comment: specific profile,
        then best resolution, then JPEG, then first available stream.

        Args:
            camera: Camera whose RTSP stream will be captured.

        Returns:
            Tuple of (file path, capture time, error). On failure the path
            and time are ``None`` and the error string is populated.
        """
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
                return None, None, err or "No RTSP stream found"

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
                return None, None, f"ffmpeg failed: {error_msg}"

            file_path, captured_at = await self._save_image(camera, stdout)
            return file_path, captured_at, None
        except FileNotFoundError:
            return None, None, "ffmpeg not found in PATH"
        except Exception as e:
            return None, None, str(e)

    async def capture(self, camera: Camera) -> Snapshot:
        """Capture a snapshot trying every available strategy in order.

        Strategies are attempted sequentially: direct URL, ONVIF
        ``GetSnapshotUri``, then RTSP+ffmpeg. The first success is
        persisted and returned. When all fail an error snapshot record is
        stored.

        Args:
            camera: Camera to capture a snapshot from.

        Returns:
            The persisted ``Snapshot`` (``status`` is ``"success"`` or
            ``"error"``).
        """
        # Strategy 1: direct URL
        if camera.snapshot_url:
            file_path, captured_at, error = await self._capture_direct_url(camera)
            if file_path:
                snapshot = Snapshot(
                    camera_id=camera.id,
                    image_path=str(file_path.relative_to(settings.snapshots_dir)),
                    file_size=file_path.stat().st_size,
                    captured_at=captured_at,
                    status="success",
                )
                logger.info(f"Camera {camera.name}: direct URL snapshot saved")
                await self._uow.snapshots.add(snapshot)
                return snapshot
            last_error = error or "direct URL failed"

        # Strategy 2: ONVIF GetSnapshotUri
        file_path, captured_at, error = await self._capture_onvif(camera)
        if file_path:
            snapshot = Snapshot(
                camera_id=camera.id,
                image_path=str(file_path.relative_to(settings.snapshots_dir)),
                file_size=file_path.stat().st_size,
                captured_at=captured_at,
                status="success",
            )
            logger.info(f"Camera {camera.name}: ONVIF snapshot saved to {file_path}")
            await self._uow.snapshots.add(snapshot)
            return snapshot
        last_error = error

        # Strategy 3: RTSP+ffmpeg fallback
        file_path, captured_at, error = await self._capture_rtsp(camera)
        if file_path:
            snapshot = Snapshot(
                camera_id=camera.id,
                image_path=str(file_path.relative_to(settings.snapshots_dir)),
                file_size=file_path.stat().st_size,
                captured_at=captured_at,
                status="success",
            )
            logger.info(f"Camera {camera.name}: RTSP snapshot saved to {file_path}")
            await self._uow.snapshots.add(snapshot)
            return snapshot
        last_error = error

        snapshot = Snapshot(
            camera_id=camera.id,
            image_path="",
            status="error",
            error_message=last_error or "All capture methods failed",
        )
        logger.error(f"Camera {camera.name}: all capture methods failed: {snapshot.error_message}")
        await self._uow.snapshots.add(snapshot)
        return snapshot

    async def force_capture(self, camera_id: int) -> Snapshot | None:
        """Trigger an immediate capture by camera id.

        Args:
            camera_id: Primary key of the camera to capture.

        Returns:
            The captured ``Snapshot`` or ``None`` when the camera is
            missing.
        """
        camera = await self._uow.cameras.get_by_id(camera_id)
        if not camera:
            logger.warning(f"Force capture: camera {camera_id} not found")
            return None
        logger.info(f"Force capture triggered for camera {camera.name} ({camera.host})")
        return await self.capture(camera)

    async def get_daily_report(self, target_date: date):
        """Group all snapshots for a date by camera.

        Args:
            target_date: The date to query (in project timezone).

        Returns:
            Dict keyed by camera_id with camera name and snapshot list,
            or empty dict if no snapshots exist.
        """
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
        """List snapshots for a single camera on a given date.

        Args:
            camera_id: Primary key of the camera.
            target_date: Date to filter snapshots by.

        Returns:
            Snapshots captured for that camera on that date (may be empty).
        """
        snapshots = await self._uow.snapshots.get_by_camera_and_date(camera_id, target_date)
        logger.debug(f"Camera {camera_id} snapshots on {target_date}: {len(snapshots)}")
        return snapshots

    async def get_dashboard_data(self):
        """Build dashboard payload of cameras and their last snapshot.

        Returns:
            List of dicts merging validated camera data with its
            ``last_snapshot`` (or ``None`` when none exists).
        """
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

    async def generate_daily_video(
        self, camera_id: int, target_date: date, hour: int | None = None
    ) -> tuple[Path, Path]:
        """Render a timelapse MP4 from a camera's snapshots for a date.

        Args:
            camera_id: Primary key of the camera.
            target_date: Date of snapshots to include.
            hour: Optional hour (0-23) to filter snapshots to a single
                hour bucket. When omitted, all snapshots for the date are
                included.

        Returns:
            Tuple of (output video path, temp working directory).

        Raises:
            ValueError: When no snapshots or image files exist for the
                given camera, date, and optional hour.
            RuntimeError: When ffmpeg fails to produce the video.
        """
        snapshots = await self._uow.snapshots.get_by_camera_and_date(camera_id, target_date)
        if hour is not None:
            snapshots = [s for s in snapshots if s.captured_at.hour == hour]
        snapshots.sort(key=lambda s: s.captured_at)
        if not snapshots:
            label = f"hour {hour:02d}:00 of {target_date}" if hour is not None else str(target_date)
            raise ValueError(f"No snapshots for camera {camera_id} on {label}")

        temp_dir = Path(tempfile.mkdtemp(prefix=f"tl_{camera_id}_"))
        file_list = temp_dir / "files.txt"

        entries = []
        for snap in snapshots:
            full_path = settings.snapshots_dir / snap.image_path
            if full_path.exists():
                entries.append(full_path)
        with open(file_list, "w") as f:
            for i, path in enumerate(entries):
                f.write(f"file '{path}'\n")
                if i < len(entries) - 1:
                    f.write("duration 0.1\n")

        if file_list.stat().st_size == 0:
            shutil.rmtree(temp_dir, ignore_errors=True)
            label = f"hour {hour:02d}:00 of {target_date}" if hour is not None else str(target_date)
            raise ValueError(f"No image files found for camera {camera_id} on {label}")

        suffix = f"_h{hour:02d}" if hour is not None else ""
        output_path = temp_dir / f"timelapse_{camera_id}_{target_date.isoformat()}{suffix}.mp4"

        proc = await asyncio.create_subprocess_exec(
            'ffmpeg',
            '-y',
            '-f', 'concat',
            '-safe', '0',
            '-i', str(file_list),
            '-c:v', 'libx264',
            '-preset', 'ultrafast',
            '-crf', '28',
            '-pix_fmt', 'yuv420p',
            '-movflags', '+faststart',
            str(output_path),
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        _, stderr = await proc.communicate()
        if proc.returncode != 0:
            shutil.rmtree(temp_dir, ignore_errors=True)
            error_msg = stderr.decode(errors='replace')[-500:] if stderr else "ffmpeg error"
            raise RuntimeError(f"Video generation failed: {error_msg}")

        label = f"camera {camera_id} on {target_date}" + (f" hour {hour:02d}" if hour is not None else "")
        logger.info(f"Video generated for {label}: {output_path}")
        return output_path, temp_dir