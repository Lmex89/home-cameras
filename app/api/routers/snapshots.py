from datetime import date
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import FileResponse
from loguru import logger

from app.api.deps import get_snapshot_service
from app.application.services.snapshot_service import SnapshotService
from app.core.config import settings
from app.domain.schemas import SnapshotRead

router = APIRouter(prefix="/api/snapshots", tags=["snapshots"])


@router.get("/{snapshot_id}", response_model=SnapshotRead)
async def get_snapshot(
    snapshot_id: int,
    service: SnapshotService = Depends(get_snapshot_service),
):
    snap = await service._uow.snapshots.get_by_id(snapshot_id)
    if not snap:
        logger.warning(f"Snapshot {snapshot_id} not found")
        raise HTTPException(status_code=404, detail="Snapshot not found")
    return SnapshotRead.model_validate(snap)


@router.get("/{camera_id}/by-date")
async def get_camera_snapshots(
    camera_id: int,
    snapshot_date: date,
    service: SnapshotService = Depends(get_snapshot_service),
):
    snapshots = await service.get_camera_snapshots(camera_id, snapshot_date)
    logger.debug(f"Camera {camera_id} snapshots on {snapshot_date}: {len(snapshots)}")
    return [SnapshotRead.model_validate(s) for s in snapshots]


@router.get("/image/{snapshot_id}")
async def get_snapshot_image(
    snapshot_id: int,
    service: SnapshotService = Depends(get_snapshot_service),
):
    snap = await service._uow.snapshots.get_by_id(snapshot_id)
    if not snap:
        logger.warning(f"Snapshot image: snapshot {snapshot_id} not found")
        raise HTTPException(status_code=404, detail="Snapshot not found")
    full_path = settings.snapshots_dir / snap.image_path
    if not full_path.exists():
        logger.warning(f"Snapshot image file not found: {full_path}")
        raise HTTPException(status_code=404, detail="Image file not found")
    logger.debug(f"Serving snapshot image: {full_path}")
    return FileResponse(str(full_path), media_type="image/jpeg")
