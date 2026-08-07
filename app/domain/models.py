"""SQLAlchemy ORM models for the cameras domain.

Defines the ``Base`` declarative base, the ``Camera`` and ``Snapshot``
entities, plus the ``AnalysisJob`` and ``SnapshotAnalysis`` models for
the ML analysis pipeline.
"""

from datetime import datetime

from sqlalchemy import Boolean, DateTime, Float, ForeignKey, Integer, Text, func
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, relationship


class Base(DeclarativeBase):
    """Declarative base shared by all ORM models in the project."""


class Camera(Base):
    """Represent an ONVIF-capable camera under monitoring.

    Stores connection details, capture scheduling preferences, and the
    one-to-many relationship to its captured snapshots.
    """

    __tablename__ = "cameras"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    name: Mapped[str] = mapped_column(Text, nullable=False)
    host: Mapped[str] = mapped_column(Text, nullable=False)
    port: Mapped[int] = mapped_column(Integer, nullable=False, default=80)
    username: Mapped[str] = mapped_column(Text, nullable=False, default="")
    password: Mapped[str] = mapped_column(Text, nullable=False, default="")
    profile_token: Mapped[str | None] = mapped_column(Text, nullable=True)
    snapshot_url: Mapped[str | None] = mapped_column(Text, nullable=True)
    interval_seconds: Mapped[int] = mapped_column(Integer, nullable=False, default=60)
    enabled: Mapped[bool] = mapped_column(Boolean, nullable=False, default=True)
    created_at: Mapped[datetime] = mapped_column(
        DateTime, nullable=False, server_default=func.current_timestamp()
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime,
        nullable=False,
        server_default=func.current_timestamp(),
        onupdate=func.current_timestamp(),
    )

    snapshots: Mapped[list["Snapshot"]] = relationship(
        "Snapshot", back_populates="camera", cascade="all, delete-orphan"
    )


class Snapshot(Base):
    """Represent a single captured image from a camera.

    Records the filesystem path, metadata, and capture status for each
    snapshot attempt, linked back to the owning ``Camera``.
    """

    __tablename__ = "snapshots"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    camera_id: Mapped[int] = mapped_column(
        Integer, ForeignKey("cameras.id", ondelete="CASCADE"), nullable=False
    )
    captured_at: Mapped[datetime] = mapped_column(
        DateTime, nullable=False, server_default=func.current_timestamp()
    )
    image_path: Mapped[str] = mapped_column(Text, nullable=False)
    file_size: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    status: Mapped[str] = mapped_column(Text, nullable=False, default="success")
    error_message: Mapped[str | None] = mapped_column(Text, nullable=True)
    archive_path: Mapped[str | None] = mapped_column(Text, nullable=True)

    camera: Mapped[Camera] = relationship("Camera", back_populates="snapshots")
    analyses: Mapped[list["SnapshotAnalysis"]] = relationship(
        "SnapshotAnalysis", back_populates="snapshot", cascade="all, delete-orphan"
    )


class AnalysisJob(Base):
    """An async work item queued for ML model inference.

    Each job links to a snapshot and a specific pipeline stage (e.g.
    ``yolo_detection``, ``anomaly_scoring``). The scheduler picks pending
    jobs and processes them via the ``AnalysisService``.
    """

    __tablename__ = "analysis_jobs"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    snapshot_id: Mapped[int] = mapped_column(
        Integer, ForeignKey("snapshots.id", ondelete="CASCADE"), nullable=False
    )
    job_type: Mapped[str] = mapped_column(Text, nullable=False, default="yolo_detection")
    status: Mapped[str] = mapped_column(Text, nullable=False, default="pending")
    priority: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    attempts: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    max_attempts: Mapped[int] = mapped_column(Integer, nullable=False, default=3)
    error_message: Mapped[str | None] = mapped_column(Text, nullable=True)
    requested_at: Mapped[datetime] = mapped_column(
        DateTime, nullable=False, server_default=func.current_timestamp()
    )
    started_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)


class SnapshotAnalysis(Base):
    """Store the ML inference result for a single snapshot + model pair.

    One row per successful analysis run. The combination of
    ``(snapshot_id, model_name)`` is unique so re-analysis overwrites the
    previous result.
    """

    __tablename__ = "snapshot_analyses"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    snapshot_id: Mapped[int] = mapped_column(
        Integer, ForeignKey("snapshots.id", ondelete="CASCADE"), nullable=False
    )
    model_name: Mapped[str] = mapped_column(Text, nullable=False)
    model_version: Mapped[str] = mapped_column(Text, nullable=False, default="1.0")
    status: Mapped[str] = mapped_column(Text, nullable=False, default="pending")
    objects_json: Mapped[str | None] = mapped_column(Text, nullable=True)
    person_count: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    review_required: Mapped[bool] = mapped_column(Boolean, nullable=False, default=False)
    review_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    anomaly_score: Mapped[float | None] = mapped_column(Float, nullable=True)
    error_message: Mapped[str | None] = mapped_column(Text, nullable=True)
    analyzed_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)
    created_at: Mapped[datetime] = mapped_column(
        DateTime, nullable=False, server_default=func.current_timestamp()
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime,
        nullable=False,
        server_default=func.current_timestamp(),
        onupdate=func.current_timestamp(),
    )

    snapshot: Mapped[Snapshot] = relationship("Snapshot", back_populates="analyses")