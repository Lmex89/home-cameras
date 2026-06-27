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
    default_interval_seconds: int = 60
    timezone: str = "America/Mexico_City"

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
