"""FastAPI router for human review management endpoints.

Exposes endpoints to list pending reviews, update review decisions,
and retrieve analysis details for flagged snapshots.
"""

from fastapi import APIRouter, Depends, HTTPException, Query
from pydantic import BaseModel
from loguru import logger

from app.api.deps import get_analysis_service
from app.application.services.analysis_service import AnalysisService
from app.domain.schemas import AnalysisReviewUpdate, PendingReviewItem, SnapshotAnalysisRead

router = APIRouter(prefix="/api/reviews", tags=["reviews"])


@router.get("/pending", response_model=list[PendingReviewItem])
async def list_pending_reviews(
    limit: int = 50,
    service: AnalysisService = Depends(get_analysis_service),
):
    """List all snapshots flagged for human review.

    \f
    Returns snapshots that the rule engine flagged, ordered by most
    recently analysed. Each item includes camera metadata, detection
    details, and the reason the review was requested.

    Args:
        limit: Maximum number of pending review items to return.
    """
    items = await service.get_pending_reviews(limit=limit)
    logger.debug(f"Pending reviews: {len(items)} items")
    return items


@router.get("/count")
async def count_pending_reviews(
    service: AnalysisService = Depends(get_analysis_service),
):
    """Return the number of snapshots currently awaiting review.

    \f
    Used by the dashboard to display a review badge count.
    """
    count = await service.count_pending_reviews()
    return {"count": count}


@router.get("/detections")
async def list_detections(
    days_back: int = Query(default=1, ge=0, le=30),
    camera_id: int | None = Query(default=None),
    class_name: str | None = Query(default=None),
    limit: int = Query(default=500, ge=1, le=2000),
    offset: int = Query(default=0, ge=0),
    date_from: str | None = Query(default=None, pattern=r"^\d{4}-\d{2}-\d{2}$"),
    service: AnalysisService = Depends(get_analysis_service),
):
    """Fetch all analyses with object detections, paginated.

    Lightweight endpoint for the detections browser. Returns only
    analyses where YOLO found objects, joined with camera and
    snapshot metadata.

    \f
    Args:
        days_back: Number of past days to include (ignored if date_from set).
        camera_id: Filter by camera.
        class_name: Filter analyses whose objects_json contains this string.
        limit: Max rows (cap 2000).
        offset: Pagination offset.
        date_from: Specific date YYYY-MM-DD — overrides days_back.

    Returns:
        List of detection items with analysis, camera, and snapshot data.
    """
    items = await service.get_detections(
        days_back=days_back,
        camera_id=camera_id,
        class_name=class_name,
        limit=limit,
        offset=offset,
        date_from=date_from,
    )
    return items


class BulkReviewPayload(BaseModel):
    """Payload for bulk review updates."""
    analysis_ids: list[int]
    review_required: bool = False
    review_reason: str | None = None


@router.post("/bulk-review")
async def bulk_update_review(
    payload: BulkReviewPayload,
    service: AnalysisService = Depends(get_analysis_service),
):
    """Update review status for multiple analyses at once.

    \f
    Args:
        payload: List of analysis IDs and the desired review state.

    Returns:
        Dict with ``updated`` count and ``errors`` list.
    """
    updated = 0
    errors = []
    for aid in payload.analysis_ids:
        try:
            analysis = await service.update_review(
                aid,
                review_required=payload.review_required,
                review_reason=payload.review_reason,
            )
            if analysis:
                updated += 1
            else:
                errors.append({"id": aid, "error": "Not found"})
        except Exception as e:
            errors.append({"id": aid, "error": str(e)})
    logger.info(f"Bulk review: {updated} updated, {len(errors)} errors")
    return {"updated": updated, "errors": errors}


@router.post("/{analysis_id}/review")
async def update_review(
    analysis_id: int,
    payload: AnalysisReviewUpdate,
    service: AnalysisService = Depends(get_analysis_service),
):
    """Update the review decision for a flagged snapshot analysis.

    \f
    Mark a snapshot as confirmed (no longer requires review), rejected
    (false positive), or change the review reason.

    Args:
        analysis_id: The unique identifier of the snapshot analysis.
        payload: The review update payload.

    Returns:
        The updated analysis summary.

    Raises:
        HTTPException: 404 if the analysis does not exist.
    """
    analysis = await service.update_review(
        analysis_id,
        review_required=payload.review_required,
        review_reason=payload.review_reason,
    )
    if not analysis:
        logger.warning(f"Review update: analysis {analysis_id} not found")
        raise HTTPException(status_code=404, detail="Analysis not found")
    logger.info(f"Review updated for analysis {analysis_id}: review_required={payload.review_required}")
    return SnapshotAnalysisRead.model_validate(analysis)
