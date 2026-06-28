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
        existing_by_name = {c.name: c for c in existing}

        with open(yaml_path) as f:
            data = yaml.safe_load(f)

        raw_cameras = data.get("cameras", [])
        if not raw_cameras:
            logger.warning(f"No cameras defined in YAML at {yaml_path}")
            return False

        yaml_names = {raw["name"] for raw in raw_cameras}
        for raw in raw_cameras:
            name = raw["name"]
            values = {
                "host": raw["host"],
                "port": raw.get("port", 80),
                "username": raw.get("username", ""),
                "password": raw.get("password", ""),
                "profile_token": raw.get("profile_token"),
                "snapshot_url": raw.get("snapshot_url"),
                "interval_seconds": raw.get("interval_seconds", settings.default_interval_seconds),
                "enabled": raw.get("enabled", True),
            }
            if name in existing_by_name:
                updated = await uow.cameras.update(existing_by_name[name].id, values)
                if updated:
                    logger.info(f"Updated camera: {updated.name} ({updated.host})")
            else:
                camera = Camera(name=name, **values)
                await uow.cameras.add(camera)
                logger.info(f"Seeded camera: {camera.name} ({camera.host})")

        for name, cam in existing_by_name.items():
            if name not in yaml_names:
                await uow.cameras.delete(cam.id)
                logger.info(f"Removed camera not in YAML: {name}")

    logger.info(f"Seed complete: {len(raw_cameras)} cameras synced from YAML")
    return True