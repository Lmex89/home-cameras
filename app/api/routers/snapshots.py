"""FastAPI router for snapshot retrieval endpoints.

Exposes endpoints to fetch snapshot metadata, query snapshots by date
for a camera, and stream snapshot image files (including from archived
ZIP storage).
"""

import zipfile
from datetime import date
from io import BytesIO
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import FileResponse, Response
from loguru import logger

from app.api.deps import get_snapshot_service
from app.application.services.snapshot_service import SnapshotService
from app.core.config import settings
from app.domain.schemas import SnapshotRead

router = APIRouter(prefix="/api/snapshots", tags=["snapshots"])


def _read_from_archive(archive_ref: str) -> bytes:
    """Extract a file from a ZIP archive given an archive reference.

    The reference format is ``{rel_zip_path}::{filename}``, e.g.
    ``snapshots/3/2025-06-15.zip::150322.jpg``.

    Args:
        archive_ref: Archive reference string stored in the snapshot row.

    Returns:
        The raw bytes of the extracted file.

    Raises:
        FileNotFoundError: When the archive ZIP or the file inside it
            does not exist.
    """
    zip_rel, filename = archive_ref.split("::", 1)
    zip_abs = settings.archives_dir / zip_rel
    if not zip_abs.exists():
        raise FileNotFoundError(f"Archive not found: {zip_abs}")
    with zipfile.ZipFile(zip_abs, "r") as zf:
        if filename not in zf.namelist():
            raise FileNotFoundError(f"{filename} not found in {zip_abs}")
        return zf.read(filename)


@router.get("/{snapshot_id}", response_model=SnapshotRead)
async def get_snapshot(
    snapshot_id: int,
    service: SnapshotService = Depends(get_snapshot_service),
):
    """Retrieve a single snapshot by its identifier.

    \f
    Args:
        snapshot_id: The unique identifier of the snapshot.

    Returns:
        The serialized snapshot record.

    Raises:
        HTTPException: 404 if the snapshot does not exist.
    """
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
    """List all snapshots for a camera on a given date.

    \f
    Args:
        camera_id: The unique identifier of the camera.
        snapshot_date: The date to query snapshots for.

    Returns:
        A list of serialized snapshot records captured on that date.
    """
    snapshots = await service.get_camera_snapshots(camera_id, snapshot_date)
    logger.debug(f"Camera {camera_id} snapshots on {snapshot_date}: {len(snapshots)}")
    return [SnapshotRead.model_validate(s) for s in snapshots]


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
            data = _read_from_archive(snap.archive_path)
            logger.debug(f"Serving snapshot {snapshot_id} from archive {snap.archive_path}")
            return Response(content=data, media_type="image/jpeg")
        except FileNotFoundError:
            logger.warning(f"Archive file missing for snapshot {snapshot_id}: {snap.archive_path}")
        except Exception as e:
            logger.exception(f"Failed to read snapshot {snapshot_id} from archive: {e}")

    logger.warning(f"Snapshot image file not found: {full_path}, archive={snap.archive_path}")
    raise HTTPException(status_code=404, detail="Image file not found")
