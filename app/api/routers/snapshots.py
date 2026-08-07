"""FastAPI router for snapshot retrieval endpoints.

Exposes endpoints to fetch snapshot metadata (including ML analysis),
query snapshots by date for a camera, and stream snapshot image files
(including from archived ZIP storage).
"""

from datetime import date

from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import FileResponse, Response
from loguru import logger

from app.api.deps import get_snapshot_service
from app.application.services.snapshot_service import SnapshotService
from app.core.config import settings
from app.domain.schemas import SnapshotRead, SnapshotWithAnalysis, SnapshotAnalysisRead
from app.infrastructure.archive import read_snapshot_from_archive

router = APIRouter(prefix="/api/snapshots", tags=["snapshots"])


@router.get("/{snapshot_id}", response_model=SnapshotWithAnalysis)
async def get_snapshot(
    snapshot_id: int,
    service: SnapshotService = Depends(get_snapshot_service),
):
    """Retrieve a single snapshot by its identifier, including analysis data.

    \f
    Args:
        snapshot_id: The unique identifier of the snapshot.

    Returns:
        The serialized snapshot record with optional ML analysis.

    Raises:
        HTTPException: 404 if the snapshot does not exist.
    """
    snap = await service._uow.snapshots.get_by_id(snapshot_id)
    if not snap:
        logger.warning(f"Snapshot {snapshot_id} not found")
        raise HTTPException(status_code=404, detail="Snapshot not found")
    snap_read = SnapshotRead.model_validate(snap)
    analyses = await service._uow.snapshot_analyses.get_by_snapshot(snapshot_id)
    analysis = SnapshotAnalysisRead.model_validate(analyses[0]) if analyses else None
    return SnapshotWithAnalysis(
        **snap_read.model_dump(),
        analysis=analysis,
    )


@router.get("/{camera_id}/by-date", response_model=list[SnapshotWithAnalysis])
async def get_camera_snapshots(
    camera_id: int,
    snapshot_date: date,
    service: SnapshotService = Depends(get_snapshot_service),
):
    """List all snapshots for a camera on a given date, with analysis data.

    \f
    Args:
        camera_id: The unique identifier of the camera.
        snapshot_date: The date to query snapshots for.

    Returns:
        A list of serialized snapshot records with ML analysis captured on
        that date.
    """
    snapshots = await service.get_camera_snapshots(camera_id, snapshot_date)
    logger.debug(f"Camera {camera_id} snapshots on {snapshot_date}: {len(snapshots)}")
    snapshot_ids = [s.id for s in snapshots]
    analyses = await service._uow.snapshot_analyses.get_by_camera_and_date(camera_id, snapshot_ids)
    analysis_by_snapshot: dict[int, SnapshotAnalysisRead | None] = {}
    for a in analyses:
        analysis_by_snapshot[a.snapshot_id] = SnapshotAnalysisRead.model_validate(a) if a else None

    result = []
    for s in snapshots:
        snap_read = SnapshotRead.model_validate(s)
        result.append(SnapshotWithAnalysis(
            **snap_read.model_dump(),
            analysis=analysis_by_snapshot.get(s.id),
        ))
    return result


@router.get("/image/{snapshot_id}")
async def get_snapshot_image(
    snapshot_id: int,
    service: SnapshotService = Depends(get_snapshot_service),
):
    """Stream a snapshot's JPEG image file.

    \f
    Args:
        snapshot_id: The unique identifier of the snapshot.

    Returns:
        A FileResponse serving the JPEG image.

    Raises:
        HTTPException: 404 if the snapshot record or image file is missing.
    """
    snap = await service._uow.snapshots.get_by_id(snapshot_id)
    if not snap:
        logger.warning(f"Snapshot image: snapshot {snapshot_id} not found")
        raise HTTPException(status_code=404, detail="Snapshot not found")

    full_path = settings.snapshots_dir / snap.image_path
    if full_path.exists():
        logger.debug(f"Serving snapshot image: {full_path}")
        return FileResponse(str(full_path), media_type="image/jpeg")

    # Try serving from archive if the raw file was rotated away
    if snap.archive_path:
        try:
            data = read_snapshot_from_archive(snap.archive_path)
            logger.debug(f"Serving snapshot {snapshot_id} from archive {snap.archive_path}")
            return Response(content=data, media_type="image/jpeg")
        except FileNotFoundError:
            logger.warning(f"Archive file missing for snapshot {snapshot_id}: {snap.archive_path}")
        except Exception as e:
            logger.exception(f"Failed to read snapshot {snapshot_id} from archive: {e}")

    logger.warning(f"Snapshot image file not found: {full_path}, archive={snap.archive_path}")
    raise HTTPException(status_code=404, detail="Image file not found")
