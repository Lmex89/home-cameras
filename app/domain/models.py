"""SQLAlchemy ORM models for the cameras domain.

Defines the ``Base`` declarative base, the ``Camera`` entity, and the
``Snapshot`` entity together with their one-to-many relationship.
"""

from datetime import datetime

from sqlalchemy import Boolean, DateTime, ForeignKey, Integer, Text, func
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