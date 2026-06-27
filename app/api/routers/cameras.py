from fastapi import APIRouter, Depends, HTTPException
from loguru import logger

from app.api.deps import get_camera_service, get_snapshot_service
from app.application.services.camera_service import CameraService
from app.application.services.snapshot_service import SnapshotService
from app.domain.schemas import (
    CameraCreate,
    CameraRead,
    CameraUpdate,
    CameraTestResult,
    CameraWithLastSnapshot,
    SnapshotForceResult,
)

router = APIRouter(prefix="/api/cameras", tags=["cameras"])


@router.get("", response_model=list[CameraWithLastSnapshot])
async def list_cameras(
    service: CameraService = Depends(get_camera_service),
    snap_service: SnapshotService = Depends(get_snapshot_service),
):
    data = await snap_service.get_dashboard_data()
    logger.debug(f"List cameras returned {len(data)} entries")
    return data


@router.get("/{camera_id}", response_model=CameraRead)
async def get_camera(
    camera_id: int,
    service: CameraService = Depends(get_camera_service),
):
    camera = await service.get_camera(camera_id)
    if not camera:
        logger.warning(f"Camera {camera_id} not found (GET)")
        raise HTTPException(status_code=404, detail="Camera not found")
    return CameraRead.model_validate(camera)


@router.post("", response_model=CameraRead, status_code=201)
async def create_camera(
    data: CameraCreate,
    service: CameraService = Depends(get_camera_service),
):
    camera = await service.create_camera(data)
    logger.info(f"Camera created via API: {camera.name} (id={camera.id})")
    return CameraRead.model_validate(camera)


@router.put("/{camera_id}", response_model=CameraRead)
async def update_camera(
    camera_id: int,
    data: CameraUpdate,
    service: CameraService = Depends(get_camera_service),
):
    camera = await service.update_camera(camera_id, data)
    if not camera:
        logger.warning(f"Camera {camera_id} not found (PUT)")
        raise HTTPException(status_code=404, detail="Camera not found")
    logger.info(f"Camera {camera_id} updated via API")
    return CameraRead.model_validate(camera)


@router.delete("/{camera_id}", status_code=204)
async def delete_camera(
    camera_id: int,
    service: CameraService = Depends(get_camera_service),
):
    deleted = await service.delete_camera(camera_id)
    if not deleted:
        logger.warning(f"Camera {camera_id} not found (DELETE)")
        raise HTTPException(status_code=404, detail="Camera not found")
    logger.info(f"Camera {camera_id} deleted via API")


@router.post("/test", response_model=CameraTestResult)
async def test_camera(data: CameraCreate):
    from app.infrastructure.onvif import ONVIFCameraClient
    onvif = ONVIFCameraClient()
    logger.info(f"Testing camera {data.host}:{data.port} via API")
    reachable, profiles, error = onvif.test_connection(
        data.host, data.port, data.username, data.password
    )
    return CameraTestResult(reachable=reachable, profiles=profiles, error=error)


@router.post("/{camera_id}/snapshot", response_model=SnapshotForceResult)
async def force_snapshot(
    camera_id: int,
    service: SnapshotService = Depends(get_snapshot_service),
    camera_service: CameraService = Depends(get_camera_service),
):
    snapshot = await service.force_capture(camera_id)
    if not snapshot:
        logger.warning(f"Force snapshot: camera {camera_id} not found")
        raise HTTPException(status_code=404, detail="Camera not found")
    camera = await camera_service.get_camera(camera_id)
    logger.info(f"Force snapshot completed for camera {camera_id}: status={snapshot.status}")
    return SnapshotForceResult(
        camera_id=snapshot.camera_id,
        camera_name=camera.name if camera else "",
        success=snapshot.status == "success",
        image_path=snapshot.image_path if snapshot.status == "success" else None,
        error=snapshot.error_message,
    )
