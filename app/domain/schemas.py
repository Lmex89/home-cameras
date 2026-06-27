from datetime import datetime
from typing import Annotated

from pydantic import BaseModel, ConfigDict, Field, StringConstraints


class CameraCreate(BaseModel):
    name: Annotated[str, StringConstraints(min_length=1, strip_whitespace=True)]
    host: Annotated[str, StringConstraints(min_length=1, strip_whitespace=True)]
    port: int = Field(default=80, ge=1, le=65535)
    username: str = ""
    password: str = ""
    profile_token: str | None = None
    interval_minutes: int = Field(default=15, ge=1)
    enabled: bool = True


class CameraUpdate(BaseModel):
    name: Annotated[str, StringConstraints(min_length=1, strip_whitespace=True)] | None = None
    host: Annotated[str, StringConstraints(min_length=1, strip_whitespace=True)] | None = None
    port: int | None = Field(default=None, ge=1, le=65535)
    username: str | None = None
    password: str | None = None
    profile_token: str | None = None
    interval_minutes: int | None = Field(default=None, ge=1)
    enabled: bool | None = None


class CameraRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    name: str
    host: str
    port: int
    username: str
    profile_token: str | None
    interval_minutes: int
    enabled: bool
    created_at: datetime
    updated_at: datetime


class CameraTestResult(BaseModel):
    reachable: bool
    profiles: list[str] = []
    error: str | None = None


class SnapshotRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    camera_id: int
    captured_at: datetime
    image_path: str
    file_size: int
    status: str
    error_message: str | None


class SnapshotForceResult(BaseModel):
    camera_id: int
    camera_name: str
    success: bool
    image_path: str | None = None
    error: str | None = None


class DailyReportCamera(BaseModel):
    camera_id: int
    camera_name: str
    total_snapshots: int
    snapshots: list[SnapshotRead]


class DailyReport(BaseModel):
    date: str
    cameras: list[DailyReportCamera]


class CameraWithLastSnapshot(CameraRead):
    last_snapshot: SnapshotRead | None = None
