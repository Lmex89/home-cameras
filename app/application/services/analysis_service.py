"""Service that orchestrates ML model inference on snapshots.

Manages the analysis job queue, runs configured models (YOLO, and later
Anomalib), applies review rules, and persists results to the database.
"""

import json
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

from loguru import logger

from app.core.config import settings
from app.core.unit_of_work import UnitOfWork
from app.domain.models import AnalysisJob, SnapshotAnalysis, Snapshot
from app.domain.schemas import PendingReviewItem
from app.infrastructure.ml.yolo import YOLODetector


class AnalysisService:
    """Orchestrate snapshot analysis: queue, process, and review flagging.

    Receives a ``UnitOfWork`` via constructor injection and an optional
    ``YOLODetector``. When no detector is provided a default instance is
    created (gracefully degrades when ``ultralytics`` is missing).
    """

    def __init__(self, uow: UnitOfWork, detector: YOLODetector | None = None) -> None:
        """Inject the unit of work and optional detector.

        Args:
            uow: UnitOfWork wrapping the async SQLAlchemy session.
            detector: YOLODetector instance. Falls back to a default when
                None, which works in stub mode without ``ultralytics``.
        """
        self._uow = uow
        self._detector = detector if detector is not None else YOLODetector()

    async def enqueue(self, snapshot_id: int, job_type: str = "yolo_detection", priority: int = 0) -> AnalysisJob:
        """Create a pending analysis job for a snapshot.

        Args:
            snapshot_id: The snapshot to analyse.
            job_type: Pipeline stage (e.g. ``yolo_detection``).
            priority: Job priority (higher = processed first).

        Returns:
            The newly created ``AnalysisJob``.
        """
        job = AnalysisJob(
            snapshot_id=snapshot_id,
            job_type=job_type,
            priority=priority,
        )
        await self._uow.analysis_jobs.add(job)
        logger.debug(f"Analysis job queued: snapshot={snapshot_id} type={job_type}")
        return job

    async def process_next_batch(self, limit: int = 5) -> int:
        """Process pending analysis jobs from the queue.

        Args:
            limit: Maximum number of jobs to process in this batch.

        Returns:
            Number of jobs successfully processed.
        """
        jobs = await self._uow.analysis_jobs.get_pending(limit=limit)
        if not jobs:
            return 0

        processed = 0
        for job in jobs:
            try:
                await self._process_job(job)
                processed += 1
            except Exception as exc:
                logger.error(f"Analysis job {job.id} failed: {exc}")
                await self._uow.analysis_jobs.mark_failed(job.id, str(exc))
        return processed

    async def _process_job(self, job: AnalysisJob) -> None:
        """Run the appropriate analysis pipeline for a single job.

        Args:
            job: The analysis job to process.
        """
        await self._uow.analysis_jobs.mark_started(job.id)

        snapshot = await self._uow.snapshots.get_by_id(job.snapshot_id)
        if not snapshot:
            await self._uow.analysis_jobs.mark_failed(job.id, "Snapshot not found")
            return

        if job.job_type == "yolo_detection":
            await self._run_yolo(snapshot, job)
        elif job.job_type == "anomaly_scoring":
            await self._run_anomaly(snapshot, job)
        else:
            await self._uow.analysis_jobs.mark_failed(job.id, f"Unknown job_type: {job.job_type}")

        await self._uow.analysis_jobs.mark_completed(job.id)

    async def _run_yolo(self, snapshot: Snapshot, job: AnalysisJob) -> SnapshotAnalysis:
        """Run YOLO detection on a snapshot and persist results.

        Args:
            snapshot: The snapshot to analyse.
            job: The analysis job being processed.

        Returns:
            The persisted ``SnapshotAnalysis``.
        """
        image_path = settings.snapshots_dir / snapshot.image_path

        if not image_path.exists():
            analysis = SnapshotAnalysis(
                snapshot_id=snapshot.id,
                model_name="yolov8n",
                status="failed",
                error_message="Image file not found",
                analyzed_at=datetime.now(ZoneInfo(settings.timezone)),
            )
            await self._uow.snapshot_analyses.add(analysis)
            logger.warning(f"YOLO skip: image not found {image_path}")
            return analysis

        detections = await self._detector.detect(image_path)
        now = datetime.now(ZoneInfo(settings.timezone))

        person_count = sum(
            1 for d in detections if d["class_name"] == "person"
        )
        objects_json = json.dumps(detections) if detections else None

        review_required, review_reason = self._apply_review_rules(snapshot, detections, person_count)

        analysis = SnapshotAnalysis(
            snapshot_id=snapshot.id,
            model_name="yolov8n",
            model_version="1.0",
            status="completed" if not review_required else "review_pending",
            objects_json=objects_json,
            person_count=person_count,
            review_required=review_required,
            review_reason=review_reason,
            analyzed_at=now,
        )
        await self._uow.snapshot_analyses.add(analysis)
        logger.info(
            f"YOLO analysis for snapshot {snapshot.id}: "
            f"{len(detections)} objects, {person_count} persons"
            + (f" — FLAGGED: {review_reason}" if review_required else "")
        )
        return analysis

    async def _run_anomaly(self, snapshot: Snapshot, job: AnalysisJob) -> SnapshotAnalysis:
        """Run anomaly detection on a snapshot (stub for future Anomalib integration).

        Args:
            snapshot: The snapshot to analyse.
            job: The analysis job being processed.

        Returns:
            The persisted ``SnapshotAnalysis``.
        """
        now = datetime.now(ZoneInfo(settings.timezone))
        analysis = SnapshotAnalysis(
            snapshot_id=snapshot.id,
            model_name="anomalib",
            status="completed",
            analyzed_at=now,
        )
        await self._uow.snapshot_analyses.add(analysis)
        logger.debug(f"Anomaly analysis stub for snapshot {snapshot.id}")
        return analysis

    def _apply_review_rules(self, snapshot: Snapshot, detections: list[dict], person_count: int) -> tuple[bool, str | None]:
        """Apply rule-based heuristics to decide if human review is needed.

        Args:
            snapshot: The snapshot being analysed.
            detections: List of detected objects.
            person_count: Number of detected persons.

        Returns:
            Tuple of ``(review_required, review_reason)``.
        """
        captured_at = snapshot.captured_at
        hour = captured_at.hour

        reasons = []

        if person_count >= settings.review_max_person_count:
            reasons.append(f"high_person_count:{person_count}")

        if person_count > 0 and (hour >= settings.review_person_after_hour or hour < settings.review_person_before_hour):
            reasons.append(f"person_after_hours:hour={hour}")

        unexpected = [d for d in detections if d["class_name"] not in ("person", "car", "truck", "bicycle", "dog", "cat", "train")]
        if unexpected:
            classes = ", ".join(set(d["class_name"] for d in unexpected[:5]))
            reasons.append(f"unexpected_objects:{classes}")

        if reasons:
            return True, "; ".join(reasons)

        return False, None

    async def get_pending_reviews(self, limit: int = 50) -> list[PendingReviewItem]:
        """Build a list of pending review items with camera metadata.

        Args:
            limit: Maximum number of items to return.

        Returns:
            List of ``PendingReviewItem`` schemas with analysis, snapshot,
            and camera info. Empty list when nothing is flagged.
        """
        analyses = await self._uow.snapshot_analyses.get_pending_reviews(limit=limit)
        result = []
        for analysis in analyses:
            snap = await self._uow.snapshots.get_by_id(analysis.snapshot_id)
            if not snap:
                continue
            cam = await self._uow.cameras.get_by_id(snap.camera_id)
            item = PendingReviewItem(
                analysis_id=analysis.id,
                snapshot_id=snap.id,
                camera_id=snap.camera_id,
                camera_name=cam.name if cam else f"Camera {snap.camera_id}",
                captured_at=snap.captured_at,
                image_path=snap.image_path,
                model_name=analysis.model_name,
                person_count=analysis.person_count,
                review_required=analysis.review_required,
                review_reason=analysis.review_reason,
                anomaly_score=analysis.anomaly_score,
                error_message=analysis.error_message,
                objects_json=analysis.objects_json,
                analyzed_at=analysis.analyzed_at,
            )
            result.append(item)
        return result

    async def count_pending_reviews(self) -> int:
        """Return the number of snapshots currently awaiting human review.

        Returns:
            Total count of completed analyses with the review flag set.
        """
        return await self._uow.snapshot_analyses.count_pending_reviews()

    async def update_review(
        self, analysis_id: int, review_required: bool, review_reason: str | None = None
    ) -> SnapshotAnalysis | None:
        """Update the review status of an analysis.

        Args:
            analysis_id: The analysis to update.
            review_required: New review requirement flag.
            review_reason: Optional reason for the review decision.

        Returns:
            The updated ``SnapshotAnalysis`` or ``None`` if not found.
        """
        analysis = await self._uow.snapshot_analyses.get_by_id(analysis_id)
        if not analysis:
            return None
        await self._uow.snapshot_analyses.update_review(analysis_id, review_required, review_reason)
        return analysis

    async def analyze_snapshot(self, snapshot: Snapshot) -> None:
        """Convenience: enqueue YOLO analysis for a newly captured snapshot.

        Args:
            snapshot: The snapshot that was just captured.
        """
        if not settings.analysis_enabled:
            return
        await self.enqueue(snapshot.id, job_type="yolo_detection")

    async def get_detections(
        self,
        days_back: int = 1,
        camera_id: int | None = None,
        class_name: str | None = None,
        limit: int = 500,
        offset: int = 0,
        date_from: str | None = None,
    ) -> list[dict]:
        """Fetch all detections with camera and snapshot metadata.

        Args:
            days_back: Only include analyses from the last N days.
            camera_id: Optional camera ID filter.
            class_name: Optional object class filter.
            limit: Maximum rows to return.
            offset: Pagination offset.
            date_from: Specific date (YYYY-MM-DD) — overrides days_back.

        Returns:
            List of dicts with analysis, camera_name, captured_at, image_path.
        """
        rows = await self._uow.snapshot_analyses.get_detections(
            days_back=days_back,
            camera_id=camera_id,
            class_name=class_name,
            limit=limit,
            offset=offset,
            date_from=date_from,
        )
        result = []
        for a, cam_name, captured_at, image_path, camera_id in rows:
            result.append({
                "analysis_id": a.id,
                "snapshot_id": a.snapshot_id,
                "camera_id": camera_id,
                "camera_name": cam_name,
                "captured_at": captured_at.isoformat() if captured_at else None,
                "image_path": image_path,
                "model_name": a.model_name,
                "objects_json": a.objects_json,
                "person_count": a.person_count,
                "review_required": a.review_required,
                "review_reason": a.review_reason,
                "analyzed_at": a.analyzed_at.isoformat() if a.analyzed_at else None,
            })
        return result
