"""YAML-to-database seeder for cameras.

Reads ``cameras.yaml`` at startup and inserts camera records when the
database is empty, providing first-run bootstrap data.
"""

from pathlib import Path

import yaml
from loguru import logger

from app.core.config import settings
from app.core.database import session_factory
from app.core.unit_of_work import UnitOfWork
from app.domain.models import Camera


async def seed_from_yaml(yaml_path: Path) -> bool:
    """Seed the database with cameras defined in a YAML file.

    Skips seeding when the file is missing, the database already contains
    cameras, or the YAML defines none.

    Args:
        yaml_path: Path to the YAML file containing a ``cameras`` list.

    Returns:
        ``True`` when cameras were seeded, ``False`` otherwise.
    """
    if not yaml_path.exists():
        logger.warning(f"cameras.yaml not found at {yaml_path}, skipping seed")
        return False

    async with UnitOfWork(session_factory) as uow:
        existing = await uow.cameras.get_all()
        if existing:
            logger.info("Database already has cameras, skipping seed")
            return False

        with open(yaml_path) as f:
            data = yaml.safe_load(f)

        raw_cameras = data.get("cameras", [])
        if not raw_cameras:
            logger.warning(f"No cameras defined in YAML at {yaml_path}")
            return False

        for raw in raw_cameras:
            camera = Camera(
                name=raw["name"],
                host=raw["host"],
                port=raw.get("port", 80),
                username=raw.get("username", ""),
                password=raw.get("password", ""),
                profile_token=raw.get("profile_token"),
                snapshot_url=raw.get("snapshot_url"),
                interval_seconds=raw.get("interval_seconds", settings.default_interval_seconds),
                enabled=raw.get("enabled", True),
            )
            await uow.cameras.add(camera)
            logger.info(f"Seeded camera: {camera.name} ({camera.host})")

    logger.info(f"Seed complete: {len(raw_cameras)} cameras loaded")
    return True