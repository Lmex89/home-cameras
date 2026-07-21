"""Application configuration loaded from environment variables.

Defines the ``Settings`` class (pydantic-settings) that reads the ``.env``
file located at the project root and exposes commonly used directory paths
and the SQLite database URL used across the application.
"""

from pathlib import Path
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Strongly-typed application settings.

    Reads configuration from a ``.env`` file located at the project root and
    exposes derived paths (data dir, snapshots dir, SQL dir, database file)
    as well as the async SQLAlchemy database URL.
    """

    model_config = SettingsConfigDict(
        env_file=Path(__file__).resolve().parent.parent.parent / ".env",
        env_file_encoding="utf-8",
    )

    app_name: str = "Camera Monitor"
    debug: bool = True
    host: str = "0.0.0.0"
    port: int = 8004
    snapshot_retention_days: int = 30
    snapshot_zip_after_days: int = 7
    video_retention_days: int = 30
    default_interval_seconds: int = 60
    timezone: str = "America/Mexico_City"
    yolo_model_path: str = "models/yolov8n.pt"
    """Filesystem path to the YOLO weights file, relative to project root or absolute."""

    yolo_confidence_threshold: float = 0.5
    """Minimum confidence score (0–1) for a detection to be kept."""

    review_person_after_hour: int = 22
    """Hour (0–23) after which a detected person triggers a review flag."""

    review_person_before_hour: int = 6
    """Hour (0–23) before which a detected person triggers a review flag."""

    review_max_person_count: int = 5
    """Maximum persons allowed in a single frame before auto-flagging."""

    analysis_enabled: bool = True
    """Master toggle for the ML analysis pipeline. Set to ``false`` to skip all analysis."""

    analysis_interval_seconds: int = 30
    """How often (in seconds) the scheduler polls for new analysis jobs."""

    timelapse_enabled: bool = True
    """Master toggle for the daily annotated timelapse generation. Set to ``false`` to disable."""

    timelapse_hour: int = 21
    """Hour (0–23) when the daily annotated timelapse is generated."""

    timelapse_minute: int = 0
    """Minute (0–59) when the daily annotated timelapse is generated."""

    timelapse_camera_id: int = 6
    """Camera ID for which the daily annotated timelapse is generated."""

    timelapse_object_classes: str = "person,car,motorcycle"
    """Comma-separated list of object classes to annotate on the timelapse."""

    timelapse_frame_duration: float = 0.4675
    """Seconds per frame in the annotated timelapse video. Higher = slower."""

    timelapse_workers: int = 3
    """Number of parallel processes used to draw detection boxes on frames."""

    telegram_enabled: bool = False
    """Master toggle for Telegram notifications. Set to ``true`` and provide
    a bot token + chat ID to receive timelapse video reports in chat."""

    telegram_bot_token: str = ""
    """Bot token from @BotFather on Telegram."""

    telegram_chat_id: str = ""
    """Target chat ID (numeric, can be negative for groups)."""

    storage_enabled: bool = False
    """Enable S3-compatible storage for large video uploads."""

    storage_endpoint_url: str = ""
    """S3-compatible endpoint URL (e.g. Backblaze B2 S3 endpoint)."""

    storage_bucket_name: str = ""
    """Bucket name for uploaded video files."""

    storage_access_key: str = ""
    """Access key ID (Backblaze key ID in S3-compatible mode)."""

    storage_secret_key: str = ""
    """Secret access key (Backblaze application key in S3-compatible mode)."""

    storage_public_url: str = ""
    """Public base URL for uploaded files (e.g. ``https://f000.backblazeb2.com/file/bucket``)."""

    storage_region: str = "us-west-004"
    """S3 region (e.g. ``us-west-004`` for Backblaze B2)."""

    @property
    def base_dir(self) -> Path:
        """Return the project root directory."""
        return Path(__file__).resolve().parent.parent.parent

    @property
    def data_dir(self) -> Path:
        """Return the data directory used for the DB and snapshots."""
        return self.base_dir / "data"

    @property
    def snapshots_dir(self) -> Path:
        """Return the directory where snapshot images are stored."""
        return self.data_dir / "snapshots"

    @property
    def archives_dir(self) -> Path:
        """Return the directory where archived zips are stored."""
        return self.data_dir / "archives"

    @property
    def videos_dir(self) -> Path:
        """Return the directory where generated videos are stored."""
        return self.data_dir / "videos"

    @property
    def db_path(self) -> Path:
        """Return the SQLite database file path."""
        return self.data_dir / "cameras.db"

    @property
    def sql_dir(self) -> Path:
        """Return the directory containing raw SQL DDL scripts."""
        return Path(__file__).resolve().parent.parent / "sql"

    @property
    def yaml_path(self) -> Path:
        """Return the path to the cameras seed YAML file."""
        return self.base_dir / "cameras.yaml"

    @property
    def database_url(self) -> str:
        """Return the async SQLAlchemy SQLite database URL."""
        return f"sqlite+aiosqlite:///{self.db_path}"


settings = Settings()
