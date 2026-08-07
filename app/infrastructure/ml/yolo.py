"""YOLO object detection adapter.

Wraps the Ultralytics YOLO model for snapshot analysis. Gracefully
handles missing dependencies so the application works without PyTorch.
"""

from pathlib import Path

from loguru import logger

from app.core.config import settings


_detector_instance: "YOLODetector | None" = None


def get_yolo_detector() -> "YOLODetector":
    """Return a process-wide cached ``YOLODetector`` singleton.

    Loads the YOLO model once and reuses it across all analysis batches
    instead of reloading ``yolov8n.pt`` (and reinitialising PyTorch) on
    every job, which previously caused CPU/thermal spikes that froze the
    host.

    Returns:
        The shared ``YOLODetector`` instance (in stub mode when
        ``ultralytics`` is unavailable).
    """
    global _detector_instance
    if _detector_instance is None:
        _detector_instance = YOLODetector()
    return _detector_instance


class YOLODetector:
    """Run YOLO object detection on snapshot images.

    Attempts to import ``ultralytics.YOLO`` on construction. When the
    dependency is missing the detector operates in stub mode and returns
    empty results.
    """

    def __init__(self) -> None:
        self._model = None
        self._available = False
        try:
            from ultralytics import YOLO
            model_path = settings.yolo_model_path
            resolved = Path(model_path)
            if not resolved.is_absolute():
                resolved = settings.base_dir / model_path
            if not resolved.exists():
                logger.warning(f"YOLO model not found at {resolved}; using stub mode")
                return
            self._model = YOLO(str(resolved))
            self._available = True
            logger.info(f"YOLO model loaded from {model_path}")
        except ImportError:
            logger.warning("ultralytics not installed; YOLO detection disabled")
        except Exception as exc:
            logger.warning(f"Failed to load YOLO model: {exc}")

    @property
    def available(self) -> bool:
        """Whether the YOLO model is loaded and ready for inference."""
        return self._available

    async def detect(self, image_path: str | Path) -> list[dict]:
        """Run object detection on a snapshot image.

        Args:
            image_path: Path to the JPEG image file.

        Returns:
            List of detection dicts with ``class_name``, ``confidence``,
            and ``bbox`` keys. Returns an empty list when the model is
            unavailable or inference fails.
        """
        if not self._available or self._model is None:
            return []

        try:
            results = self._model(str(image_path), conf=settings.yolo_confidence_threshold, verbose=False)
            detections = []
            for result in results:
                if result.boxes is None:
                    continue
                for box in result.boxes:
                    cls_id = int(box.cls[0].item())
                    class_name = result.names[cls_id]
                    confidence = round(box.conf[0].item(), 4)
                    x1, y1, x2, y2 = [round(v.item(), 1) for v in box.xyxy[0]]
                    detections.append({
                        "class_name": class_name,
                        "confidence": confidence,
                        "bbox": [x1, y1, x2, y2],
                    })
            return detections
        except Exception as exc:
            logger.error(f"YOLO inference failed on {image_path}: {exc}")
            return []
