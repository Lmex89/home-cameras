"""Service for generating timelapse videos with object detection overlays.

Draws bounding boxes for configurable object classes (person, car, motorcycle
by default) on each snapshot frame before assembling an MP4 via ffmpeg.
"""

import asyncio
import json
import shutil
import tempfile
from datetime import date
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont
from loguru import logger

from app.core.config import settings
from app.core.unit_of_work import UnitOfWork


CLASS_COLORS: dict[str, tuple[int, int, int]] = {
    "person": (0, 191, 255),
    "car": (50, 205, 50),
    "motorcycle": (255, 140, 0),
}

_DEFAULT_COLOR = (255, 50, 50)


class TimelapseService:
    """Generate annotated timelapse videos from snapshots with detection overlays.

    Args:
        uow: Unit of Work providing database access.
    """

    def __init__(self, uow: UnitOfWork) -> None:
        self._uow = uow

    async def generate_annotated_timelapse(
        self,
        camera_id: int,
        target_date: date,
        target_classes: set[str] | None = None,
    ) -> tuple[Path, Path]:
        """Render an MP4 timelapse with detection bounding boxes overlayed.

        Fetches all snapshots for the camera on *target_date*, retrieves
        their YOLO analysis results, draws bounding boxes for the configured
        object classes, and stitches them into a video with ffmpeg.

        Snapshots without analysis results are still included (no boxes
        drawn) so the video gives full-day context.

        Args:
            camera_id: Primary key of the camera.
            target_date: Date of snapshots to include.
            target_classes: Set of class names to annotate. When ``None``,
                uses the comma-separated setting ``timelapse_object_classes``.

        Returns:
            Tuple of (output video path, temp working directory).

        Raises:
            ValueError: When no snapshots or image files exist.
            RuntimeError: When ffmpeg fails.
        """
        if target_classes is None:
            raw = settings.timelapse_object_classes
            target_classes = {c.strip() for c in raw.split(",") if c.strip()}
        logger.info(f"Step 1/6: Target classes: {target_classes}")

        snapshots = await self._uow.snapshots.get_by_camera_and_date(camera_id, target_date)
        snapshots.sort(key=lambda s: s.captured_at)
        total_snaps = len(snapshots)
        logger.info(f"Step 2/6: Found {total_snaps} snapshots for camera {camera_id} on {target_date}")
        if not snapshots:
            raise ValueError(f"No snapshots for camera {camera_id} on {target_date}")

        logger.info(f"Step 3/6: Fetching YOLO analysis for {total_snaps} snapshots...")
        analyses_by_snap: dict[int, list[dict]] = {}
        total_with = 0
        for snap in snapshots:
            analyses = await self._uow.snapshot_analyses.get_by_snapshot(snap.id)
            detections: list[dict] = []
            for a in analyses:
                if a.objects_json:
                    try:
                        parsed = json.loads(a.objects_json)
                        if isinstance(parsed, list):
                            detections = parsed
                            break
                    except json.JSONDecodeError:
                        logger.warning(f"Invalid JSON in analysis {a.id} for snapshot {snap.id}")
            analyses_by_snap[snap.id] = detections
            if detections:
                total_with += 1
        total_without = total_snaps - total_with
        logger.info(f"Step 3/6 done: {total_with} snapshots have detections, {total_without} have none")

        temp_dir = Path(tempfile.mkdtemp(prefix=f"atl_{camera_id}_"))
        frame_paths: list[Path] = []

        logger.info(f"Step 4/6: Drawing detection boxes on {total_snaps} frames...")
        for idx, snap in enumerate(snapshots):
            full_path = settings.snapshots_dir / snap.image_path
            if not full_path.exists():
                logger.debug(f"Skipping missing snapshot image: {full_path}")
                continue
            detections = analyses_by_snap.get(snap.id, [])
            annotated = self._draw_detections(full_path, detections, target_classes)
            annotated_path = temp_dir / f"{snap.id:08d}.jpg"
            annotated.save(annotated_path, "JPEG", quality=92)
            frame_paths.append(annotated_path)
            if (idx + 1) % 200 == 0 or idx == total_snaps - 1:
                pct = (idx + 1) / total_snaps * 100
                logger.info(f"  Annotated {idx + 1}/{total_snaps} frames ({pct:.0f}%)")

        if not frame_paths:
            shutil.rmtree(temp_dir, ignore_errors=True)
            raise ValueError(f"No valid image files found for camera {camera_id} on {target_date}")
        logger.info(f"Step 4/6 done: {len(frame_paths)} frames annotated")

        file_list = temp_dir / "files.txt"
        duration = str(settings.timelapse_frame_duration)
        with open(file_list, "w") as f:
            for i, path in enumerate(frame_paths):
                f.write(f"file '{path}'\n")
                if i < len(frame_paths) - 1:
                    f.write(f"duration {duration}\n")

        output_path = temp_dir / f"timelapse_annotated_{camera_id}_{target_date.isoformat()}.mp4"
        cmd = [
            "ffmpeg", "-y",
            "-f", "concat", "-safe", "0", "-i", str(file_list),
            "-c:v", "libx264", "-preset", "ultrafast", "-crf", "28",
            "-pix_fmt", "yuv420p", "-movflags", "+faststart",
            str(output_path),
        ]
        logger.info(f"Step 5/6: Running ffmpeg ({len(frame_paths)} frames, {duration}s each)...")
        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        _, stderr = await proc.communicate()
        if proc.returncode != 0:
            shutil.rmtree(temp_dir, ignore_errors=True)
            error_msg = stderr.decode(errors="replace")[-500:] if stderr else "ffmpeg error"
            logger.error(f"ffmpeg failed for annotated timelapse: camera={camera_id} returncode={proc.returncode} stderr={error_msg}")
            raise RuntimeError(f"Annotated video generation failed: {error_msg}")
        logger.info(f"Step 5/6 done: ffmpeg completed successfully")

        size_mb = output_path.stat().st_size / (1024 * 1024)
        logger.info(f"Step 6/6: Video ready — {size_mb:.1f}MB")
        return output_path, temp_dir

    @staticmethod
    def _draw_detections(
        image_path: Path,
        detections: list[dict],
        target_classes: set[str],
    ) -> Image.Image:
        """Draw bounding boxes on a snapshot image for matching class names.

        Args:
            image_path: Path to the source JPEG snapshot.
            detections: List of detection dicts with ``class_name``,
                ``bbox`` (``[x1, y1, x2, y2]``), and ``confidence``.
            target_classes: Set of class names to draw (e.g. ``{"person"}``).

        Returns:
            A new PIL Image with boxes and labels drawn.
        """
        img = Image.open(image_path).convert("RGB")
        draw = ImageDraw.Draw(img)
        try:
            font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", size=max(14, img.width // 80))
        except (OSError, IOError):
            font = ImageFont.load_default()
        width, height = img.size

        for det in detections:
            cls_name = det.get("class_name", "")
            if cls_name not in target_classes:
                continue
            confidence = det.get("confidence", 0)
            bbox = det.get("bbox")
            if not bbox or len(bbox) != 4:
                continue
            x1, y1, x2, y2 = bbox
            color = CLASS_COLORS.get(cls_name, _DEFAULT_COLOR)
            thickness = max(2, min(width, height) // 300)
            draw.rectangle([x1, y1, x2, y2], outline=color, width=thickness)
            label = f"{cls_name} {confidence:.0%}"
            bbox_text = draw.textbbox((0, 0), label, font=font)
            tw = bbox_text[2] - bbox_text[0]
            th = bbox_text[3] - bbox_text[1]
            draw.rectangle([x1, y1 - th - 4, x1 + tw + 4, y1], fill=color)
            draw.text((x1 + 2, y1 - th - 2), label, fill="white", font=font)

        return img
