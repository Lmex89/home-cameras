"""Utility functions for reading archived files from ZIP storage.

Snapshots and videos older than the zip threshold are compressed into
``data/archives/`` ZIP files. This module provides helpers to extract
files from those archives without coupling filesystem logic into
routers or services.
"""

import zipfile
from pathlib import Path

from app.core.config import settings


def read_snapshot_from_archive(archive_ref: str) -> bytes:
    """Extract a snapshot image from a ZIP archive given its reference.

    The reference format is ``{rel_zip_path}::{filename}``, e.g.
    ``snapshots/3/2025-06-15.zip::150322.jpg``.

    Args:
        archive_ref: Archive reference string stored in the snapshot row.

    Returns:
        The raw bytes of the extracted JPEG image.

    Raises:
        FileNotFoundError: When the archive ZIP or the file inside it
            does not exist.
    """
    zip_rel, filename = archive_ref.split("::", 1)
    zip_abs = settings.archives_dir / zip_rel
    if not zip_abs.exists():
        raise FileNotFoundError(f"Archive not found: {zip_abs}")
    with zipfile.ZipFile(zip_abs, "r") as zf:
        if filename not in zf.namelist():
            raise FileNotFoundError(f"{filename} not found in {zip_abs}")
        return zf.read(filename)


def read_video_from_archive(filename: str) -> bytes | None:
    """Extract a video MP4 from an archive ZIP by filename.

    Derives the archive path from the filename pattern:
    ``timelapse_{camera_id}_{date}_h{hour}.mp4`` or
    ``timelapse_{camera_id}_{date}.mp4``.

    Args:
        filename: The MP4 filename to look up in the archive.

    Returns:
        The raw bytes of the MP4 file, or None when the archive or
        file inside it is not found.
    """
    parts = Path(filename).stem.split("_")
    if len(parts) < 3:
        return None
    cam_id = parts[1]
    date_part = parts[2]
    zip_path = settings.archives_dir / "videos" / cam_id / f"{date_part}.zip"
    if not zip_path.exists():
        return None
    with zipfile.ZipFile(zip_path, "r") as zf:
        if filename not in zf.namelist():
            return None
        return zf.read(filename)
