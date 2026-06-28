"""Pydantic schemas for request validation and response serialization.

Provides input schemas (``CameraCreate``, ``CameraUpdate``), read schemas
(``CameraRead``, ``SnapshotRead``), and report/result schemas used across
the API layer.
"""

from datetime import date, datetime
from typing import Annotated

from pydantic import BaseModel, ConfigDict, Field, StringConstraints


class CameraCreate(BaseModel):
    """Validate payload for creating a new camera."""

    name: Annotated[str, StringConstraints(min_length=1, strip_whitespace=True)]
    host: Annotated[str, StringConstraints(min_length=1, strip_whitespace=True)]
    port: int = Field(default=80, ge=1, le=65535)
    username: str = ""
    password: str = ""
    profile_token: str | None = None
    snapshot_url: str | None = None
    interval_seconds: int = Field(default=60, ge=10)
    enabled: bool = True


class CameraUpdate(BaseModel):
    """Validate payload for partially updating an existing camera."""

    name: Annotated[str, StringConstraints(min_length=1, strip_whitespace=True)] | None = None
    host: Annotated[str, StringConstraints(min_length=1, strip_whitespace=True)] | None = None
    port: int | None = Field(default=None, ge=1, le=65535)
    username: str | None = None
    password: str | None = None
    profile_token: str | None = None
    snapshot_url: str | None = None
    interval_seconds: int | None = Field(default=None, ge=10)
    enabled: bool | None = None


class CameraRead(BaseModel):
    """Serialize a camera entity for API responses."""

    model_config = ConfigDict(from_attributes=True)

    id: int
    name: str
    host: str
    port: int
    username: str
    profile_token: str | None
    snapshot_url: str | None
    interval_seconds: int
    enabled: bool
    created_at: datetime
    updated_at: datetime


class CameraTestResult(BaseModel):
    """Serialize the outcome of a camera connectivity test."""

    reachable: bool
    profiles: list[str] = []
    error: str | None = None


class SnapshotRead(BaseModel):
    """Serialize a snapshot entity for API responses."""

    model_config = ConfigDict(from_attributes=True)

    id: int
    camera_id: int
    captured_at: datetime
    image_path: str
    file_size: int
    status: str
    error_message: str | None


class SnapshotForceResult(BaseModel):
    """Serialize the result of a forced snapshot capture operation."""

    camera_id: int
    camera_name: str
    success: bool
    image_path: str | None = None
    error: str | None = None


class DailyReportCamera(BaseModel):
    """Serialize one camera's section within a daily report."""

    camera_id: int
    camera_name: str
    total_snapshots: int
    snapshots: list[SnapshotRead]


class DailyReport(BaseModel):
    """Serialize the full daily snapshot report across all cameras."""

    date: str
    cameras: list[DailyReportCamera]


class CameraWithLastSnapshot(CameraRead):
    """Serialize a camera together with its most recent snapshot."""

    last_snapshot: SnapshotRead | None = None


class VideoRequest(BaseModel):
    """Validate payload for timelapse video generation."""

    camera_id: int = Field(ge=1)
    date: date
    hour: int | None = Field(default=None, ge=0, le=23)


class VideoResponse(BaseModel):
    """Serialize the result of a successful video generation."""

    video_url: str