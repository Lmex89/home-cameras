#!/usr/bin/env python3
"""Export camera and snapshot data from SQLite to a JSON manifest.

The manifest is read by the standalone dashboard (index.html) so it can
display reports without needing the FastAPI backend running.

Usage:
    .venv/bin/python tools/export_manifest.py
"""

import json
import sqlite3
from datetime import datetime
from pathlib import Path

from loguru import logger

DATA_DIR = Path(__file__).resolve().parent.parent / "data"
DB_PATH = DATA_DIR / "cameras.db"
OUTPUT_PATH = DATA_DIR / "manifest.json"


def main() -> int:
    """Export cameras and snapshots from the SQLite database to manifest.json.

    Reads the cameras.db SQLite database, groups snapshots by camera and
    date, and writes a JSON manifest file consumed by the standalone
    dashboard (index.html).

    Returns:
        Exit code 0 on success, 1 if the database file is missing.
    """
    if not DB_PATH.exists():
        logger.error(f"Database not found: {DB_PATH}")
        return 1

    conn = sqlite3.connect(str(DB_PATH))
    conn.row_factory = sqlite3.Row

    cameras = []
    for row in conn.execute(
        "SELECT id, name, host, port, interval_seconds, enabled FROM cameras ORDER BY id"
    ):
        cam = dict(row)
        cam["last_snapshot"] = None

        snap = conn.execute(
            "SELECT image_path, captured_at, file_size, status "
            "FROM snapshots WHERE camera_id = ? AND status = 'success' "
            "AND image_path != '' ORDER BY captured_at DESC LIMIT 1",
            (cam["id"],),
        ).fetchone()
        if snap:
            cam["last_snapshot"] = {
                "path": snap["image_path"],
                "captured_at": snap["captured_at"],
                "file_size": snap["file_size"],
            }

        count = conn.execute(
            "SELECT COUNT(*) FROM snapshots WHERE camera_id = ? AND status = 'success'",
            (cam["id"],),
        ).fetchone()[0]
        cam["total_snapshots"] = count
        cameras.append(cam)

    snapshots = {}
    for row in conn.execute(
        "SELECT camera_id, image_path, captured_at, file_size, status "
        "FROM snapshots WHERE status = 'success' AND image_path != '' "
        "ORDER BY camera_id, captured_at"
    ):
        cam_id = str(row["camera_id"])
        d = row["captured_at"][:10] if row["captured_at"] else "unknown"
        if cam_id not in snapshots:
            snapshots[cam_id] = {}
        if d not in snapshots[cam_id]:
            snapshots[cam_id][d] = []
        snapshots[cam_id][d].append(
            {
                "path": row["image_path"],
                "captured_at": row["captured_at"],
                "file_size": row["file_size"],
            }
        )

    manifest = {
        "generated_at": datetime.now().isoformat(),
        "cameras": cameras,
        "snapshots": snapshots,
    }

    OUTPUT_PATH.write_text(json.dumps(manifest, indent=2, default=str), encoding="utf-8")
    cam_count = len(cameras)
    snap_count = sum(c["total_snapshots"] for c in cameras)
    logger.info(f"Manifest written to {OUTPUT_PATH} ({cam_count} cameras, {snap_count} snapshots)")
    return 0


if __name__ == "__main__":
    exit(main())
