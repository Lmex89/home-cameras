from pathlib import Path
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=Path(__file__).resolve().parent.parent.parent / ".env",
        env_file_encoding="utf-8",
    )

    app_name: str = "Camera Monitor"
    debug: bool = True
    host: str = "0.0.0.0"
    port: int = 8004
    snapshot_retention_days: int = 30
    default_interval_minutes: int = 1

    @property
    def base_dir(self) -> Path:
        return Path(__file__).resolve().parent.parent.parent

    @property
    def data_dir(self) -> Path:
        return self.base_dir / "data"

    @property
    def snapshots_dir(self) -> Path:
        return self.data_dir / "snapshots"

    @property
    def db_path(self) -> Path:
        return self.data_dir / "cameras.db"

    @property
    def sql_dir(self) -> Path:
        return Path(__file__).resolve().parent.parent / "sql"

    @property
    def yaml_path(self) -> Path:
        return self.base_dir / "cameras.yaml"

    @property
    def database_url(self) -> str:
        return f"sqlite+aiosqlite:///{self.db_path}"


settings = Settings()
