from datetime import date

from fastapi import APIRouter, Depends
from loguru import logger

from app.api.deps import get_snapshot_service
from app.application.services.snapshot_service import SnapshotService
from app.domain.schemas import DailyReport, DailyReportCamera, SnapshotRead

router = APIRouter(prefix="/api/report", tags=["report"])


@router.get("/{report_date}", response_model=DailyReport)
async def get_report(
    report_date: date,
    service: SnapshotService = Depends(get_snapshot_service),
):
    logger.debug(f"Report requested for {report_date}")
    raw = await service.get_daily_report(report_date)
    cameras_list = []
    for cam_id, data in raw["cameras"].items():
        snapshots = [SnapshotRead.model_validate(s) for s in data["snapshots"]]
        cameras_list.append(
            DailyReportCamera(
                camera_id=cam_id,
                camera_name=data["name"],
                total_snapshots=len(snapshots),
                snapshots=snapshots,
            )
        )
    logger.info(f"Report for {report_date}: {len(cameras_list)} cameras")
    return DailyReport(date=raw["date"], cameras=cameras_list)
