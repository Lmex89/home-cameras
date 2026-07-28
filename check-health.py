#!/usr/bin/env python3
"""Health check for the camera-monitor service.

Queries the SQLite database for the last snapshot of a given camera,
parses the most recent log file for success/error entries, and reports
whether the capture process appears hung, frozen, or stopped.

Optionally sends a Telegram alert when the process is unhealthy.

Usage:
    python check-health.py                    # check camera 1, print report
    python check-health.py --camera 5         # check camera 5
    python check-health.py --threshold 10     # alert if last snapshot > 10 min ago
    python check-health.py --telegram         # send Telegram alert if unhealthy
    python check-health.py --watch 60         # repeat every 60 seconds
"""

import argparse
import glob
import os
import re
import sqlite3
import sys
import zipfile
from datetime import datetime, timedelta
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent
DATA_DIR = PROJECT_ROOT / "data"
DB_PATH = DATA_DIR / "cameras.db"
LOGS_DIR = DATA_DIR / "logs"

SUCCESS_PATTERN = re.compile(
    r"^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\.\d+\s*\|\s*INFO\s*\|.*snapshot (?:ok|saved)",
    re.IGNORECASE,
)
ERROR_PATTERN = re.compile(
    r"^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\.\d+\s*\|\s*ERROR\s*\|",
    re.IGNORECASE,
)
TIMEOUT_PATTERN = re.compile(
    r"^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\.\d+\s*\|\s*ERROR\s*\|.*timed out",
    re.IGNORECASE,
)


def get_last_snapshot(camera_id: int) -> dict | None:
    """Query the database for the most recent snapshot of a camera.

    Args:
        camera_id: The camera ID to query.

    Returns:
        Dict with id, captured_at, status, error_message, or None.
    """
    if not DB_PATH.exists():
        return None
    conn = sqlite3.connect(str(DB_PATH))
    conn.row_factory = sqlite3.Row
    try:
        row = conn.execute(
            "SELECT id, captured_at, status, error_message, image_path "
            "FROM snapshots WHERE camera_id = ? ORDER BY id DESC LIMIT 1",
            (camera_id,),
        ).fetchone()
        return dict(row) if row else None
    finally:
        conn.close()


def get_camera_name(camera_id: int) -> str:
    """Look up the camera name from the database.

    Args:
        camera_id: The camera ID.

    Returns:
        Camera name or 'Camera {id}' as fallback.
    """
    if not DB_PATH.exists():
        return f"Camera {camera_id}"
    conn = sqlite3.connect(str(DB_PATH))
    try:
        row = conn.execute(
            "SELECT name FROM cameras WHERE id = ?", (camera_id,)
        ).fetchone()
        return row[0] if row else f"Camera {camera_id}"
    finally:
        conn.close()


def get_recent_snapshot_count(camera_id: int, minutes: int) -> int:
    """Count successful snapshots for a camera in the last N minutes.

    Args:
        camera_id: The camera ID.
        minutes: Lookback window in minutes.

    Returns:
        Number of successful snapshots in the window.
    """
    if not DB_PATH.exists():
        return 0
    cutoff = (datetime.now() - timedelta(minutes=minutes)).isoformat()
    conn = sqlite3.connect(str(DB_PATH))
    try:
        row = conn.execute(
            "SELECT COUNT(*) FROM snapshots "
            "WHERE camera_id = ? AND status = 'success' AND captured_at >= ?",
            (camera_id, cutoff),
        ).fetchone()
        return row[0] if row else 0
    finally:
        conn.close()


def get_pending_analysis_jobs() -> int:
    """Count analysis jobs stuck in pending status.

    Returns:
        Number of pending analysis jobs.
    """
    if not DB_PATH.exists():
        return 0
    conn = sqlite3.connect(str(DB_PATH))
    try:
        row = conn.execute(
            "SELECT COUNT(*) FROM analysis_jobs WHERE status = 'pending'"
        ).fetchone()
        return row[0] if row else 0
    finally:
        conn.close()


def find_latest_log() -> Path | None:
    """Find the most recent log file (plain or zipped).

    Returns:
        Path to the latest log file, or None.
    """
    if not LOGS_DIR.exists():
        return None
    logs = sorted(LOGS_DIR.glob("app_*.log"), key=lambda p: p.stat().st_mtime, reverse=True)
    if logs:
        return logs[0]
    zips = sorted(LOGS_DIR.glob("app_*.log.zip"), key=lambda p: p.stat().st_mtime, reverse=True)
    if zips:
        return zips[0]
    return None


def parse_log_tail(log_path: Path, lines: int = 500) -> dict:
    """Parse the tail of a log file for health indicators.

    Args:
        log_path: Path to the log file.
        lines: Number of lines to read from the end.

    Returns:
        Dict with last_success_ts, last_error_ts, last_line_ts,
        recent_successes, recent_errors, timeout_errors.
    """
    result = {
        "last_success_ts": None,
        "last_error_ts": None,
        "last_line_ts": None,
        "recent_successes": 0,
        "recent_errors": 0,
        "timeout_errors": 0,
        "last_error_msg": "",
    }

    raw_lines: list[str] = []

    if str(log_path).endswith(".zip"):
        with zipfile.ZipFile(log_path) as zf:
            name = zf.namelist()[0]
            with zf.open(name) as f:
                all_lines = f.read().decode(errors="replace").splitlines()
                raw_lines = all_lines[-lines:]
    else:
        with open(log_path, errors="replace") as f:
            all_lines = f.readlines()
            raw_lines = [l.rstrip() for l in all_lines[-lines:]]

    for line in raw_lines:
        m = SUCCESS_PATTERN.match(line)
        if m:
            ts = datetime.strptime(m.group(1), "%Y-%m-%d %H:%M:%S")
            result["last_success_ts"] = ts
            result["recent_successes"] += 1

        m = ERROR_PATTERN.match(line)
        if m:
            ts = datetime.strptime(m.group(1), "%Y-%m-%d %H:%M:%S")
            result["last_error_ts"] = ts
            result["recent_errors"] += 1
            result["last_error_msg"] = line

        m = TIMEOUT_PATTERN.match(line)
        if m:
            result["timeout_errors"] += 1

        ts_match = re.match(r"^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})", line)
        if ts_match:
            result["last_line_ts"] = datetime.strptime(
                ts_match.group(1), "%Y-%m-%d %H:%M:%S"
            )

    return result


def is_service_running() -> bool:
    """Check if the camera-monitor systemd service is active.

    Returns:
        True if the service is running.
    """
    return os.system("systemctl is-active --quiet camera-monitor 2>/dev/null") == 0


def build_report(
    camera_id: int,
    camera_name: str,
    last_snap: dict | None,
    log_info: dict,
    threshold_minutes: int,
    recent_count: int,
    pending_jobs: int,
    service_running: bool,
) -> tuple[str, str]:
    """Build a human-readable health report.

    Args:
        camera_id: Camera ID checked.
        camera_name: Display name of the camera.
        last_snap: Last snapshot dict from DB.
        log_info: Parsed log tail info.
        threshold_minutes: Max age in minutes before considered unhealthy.
        recent_count: Successful snapshots in the threshold window.
        pending_jobs: Number of pending analysis jobs.
        service_running: Whether the systemd service is active.

    Returns:
        Tuple of (status_label, report_text).
        status_label is one of: 'healthy', 'warning', 'critical'.
    """
    now = datetime.now()
    lines = []
    status = "healthy"

    lines.append(f"=== Camera Monitor Health Check ===")
    lines.append(f"Camera: {camera_name} (ID {camera_id})")
    lines.append(f"Checked at: {now.strftime('%Y-%m-%d %H:%M:%S')}")
    lines.append(f"Service: {'running' if service_running else 'STOPPED'}")
    lines.append("")

    if not service_running:
        status = "critical"

    # DB snapshot check
    lines.append("--- Last Snapshot (DB) ---")
    if last_snap:
        captured = last_snap["captured_at"]
        try:
            snap_ts = datetime.fromisoformat(captured)
        except (ValueError, TypeError):
            snap_ts = None
        age_str = ""
        if snap_ts:
            age = abs(now - snap_ts)
            age_str = f" ({_human_delta(age)} ago)"
            if age > timedelta(minutes=threshold_minutes):
                status = "critical" if status != "critical" else status
        lines.append(f"  ID: {last_snap['id']}")
        lines.append(f"  Captured: {captured}{age_str}")
        lines.append(f"  Status: {last_snap['status']}")
        if last_snap.get("error_message"):
            lines.append(f"  Error: {last_snap['error_message'][:120]}")
        if last_snap["status"] == "error":
            if status == "healthy":
                status = "warning"
    else:
        lines.append("  No snapshots found")
        status = "critical"
    lines.append(f"  Recent successes (last {threshold_minutes}m): {recent_count}")
    if recent_count == 0 and last_snap:
        if status == "healthy":
            status = "warning"
    lines.append("")

    # Log check
    lines.append("--- Log Analysis ---")
    log_path = find_latest_log()
    if log_path:
        lines.append(f"  Log file: {log_path.name}")
    if log_info["last_success_ts"]:
        lines.append(f"  Last success in log: {log_info['last_success_ts']}")
    else:
        lines.append("  Last success in log: NONE")
    if log_info["last_error_ts"]:
        lines.append(f"  Last error in log: {log_info['last_error_ts']}")
    if log_info["last_line_ts"]:
        log_age = abs(now - log_info["last_line_ts"])
        lines.append(f"  Last log entry: {log_info['last_line_ts']} ({_human_delta(log_age)} ago)")
        if log_age > timedelta(minutes=threshold_minutes):
            if status == "healthy":
                status = "warning"
    lines.append(f"  Successes in tail: {log_info['recent_successes']}")
    lines.append(f"  Errors in tail: {log_info['recent_errors']}")
    if log_info["timeout_errors"]:
        lines.append(f"  Timeout errors: {log_info['timeout_errors']}")
        if status == "healthy":
            status = "warning"
    lines.append("")

    # Pending jobs
    lines.append("--- Analysis Queue ---")
    lines.append(f"  Pending jobs: {pending_jobs}")
    if pending_jobs > 20:
        lines.append(f"  WARNING: large backlog may indicate a stuck processor")
        if status == "healthy":
            status = "warning"
    lines.append("")

    # Verdict
    if status == "critical":
        verdict = "CRITICAL: Process appears hung/frozen/stopped"
    elif status == "warning":
        verdict = "WARNING: Potential issues detected"
    else:
        verdict = "HEALTHY: Process running normally"

    lines.append(f">>> {verdict} <<<")
    return status, "\n".join(lines)


def _human_delta(td: timedelta) -> str:
    """Format a timedelta into a short human-readable string.

    Args:
        td: The timedelta to format.

    Returns:
        Short string like '3m', '2h 15m', '4d 2h'.
    """
    total = int(td.total_seconds())
    if total < 0:
        return "in the future"
    days, rem = divmod(total, 86400)
    hours, rem = divmod(rem, 3600)
    minutes = rem // 60
    if days:
        return f"{days}d {hours}h"
    if hours:
        return f"{hours}h {minutes}m"
    return f"{minutes}m"


def send_telegram(text: str) -> bool:
    """Send a Telegram alert using the project's TelegramNotifier.

    Args:
        text: Message body to send.

    Returns:
        True if the message was sent successfully.
    """
    try:
        import asyncio
        from app.infrastructure.telegram import TelegramNotifier
        notifier = TelegramNotifier.from_settings()
        return asyncio.run(notifier.send_message(text))
    except Exception as e:
        print(f"Failed to send Telegram message: {e}", file=sys.stderr)
        return False


def check(
    camera_id: int = 1,
    threshold_minutes: int = 15,
    send_alert: bool = False,
) -> tuple[str, str]:
    """Run a single health check.

    Args:
        camera_id: Camera to monitor.
        threshold_minutes: Max acceptable age for the last snapshot.
        send_alert: Whether to send a Telegram alert on unhealthy status.

    Returns:
        Tuple of (status_label, report_text).
    """
    camera_name = get_camera_name(camera_id)
    last_snap = get_last_snapshot(camera_id)
    log_path = find_latest_log()
    log_info = parse_log_tail(log_path) if log_path else {
        "last_success_ts": None,
        "last_error_ts": None,
        "last_line_ts": None,
        "recent_successes": 0,
        "recent_errors": 0,
        "timeout_errors": 0,
        "last_error_msg": "",
    }
    recent_count = get_recent_snapshot_count(camera_id, threshold_minutes)
    pending_jobs = get_pending_analysis_jobs()
    service_running = is_service_running()

    status, report = build_report(
        camera_id, camera_name, last_snap, log_info,
        threshold_minutes, recent_count, pending_jobs, service_running,
    )

    if send_alert and status in ("critical", "warning"):
        alert = (
            f"Camera Monitor Alert [{status.upper()}]\n"
            f"Camera: {camera_name} (ID {camera_id})\n"
            f"Time: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n"
            f"Service: {'running' if service_running else 'STOPPED'}\n\n"
        )
        if last_snap:
            alert += f"Last snapshot: {last_snap['captured_at']} ({last_snap['status']})\n"
        else:
            alert += "Last snapshot: NONE\n"
        alert += f"Recent captures (last {threshold_minutes}m): {recent_count}\n"
        alert += f"Pending analysis jobs: {pending_jobs}\n"
        if log_info.get("timeout_errors"):
            alert += f"Timeout errors in log: {log_info['timeout_errors']}\n"
        if log_info.get("last_error_msg"):
            alert += f"Last error: {log_info['last_error_msg'][:120]}\n"
        send_telegram(alert)

    return status, report


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Check camera-monitor process health"
    )
    parser.add_argument(
        "--camera", type=int, default=1,
        help="Camera ID to check (default: 1)",
    )
    parser.add_argument(
        "--threshold", type=int, default=15,
        help="Max minutes since last snapshot before alert (default: 15)",
    )
    parser.add_argument(
        "--telegram", action="store_true",
        help="Send Telegram alert if unhealthy",
    )
    parser.add_argument(
        "--watch", type=int, default=0, metavar="SECONDS",
        help="Repeat check every N seconds (0 = run once)",
    )
    parser.add_argument(
        "--quiet", action="store_true",
        help="Only print output when status is not healthy",
    )
    args = parser.parse_args()

    if args.watch > 0:
        import time
        while True:
            status, report = check(args.camera, args.threshold, args.telegram)
            if not args.quiet or status != "healthy":
                print(report)
                print()
            time.sleep(args.watch)
    else:
        status, report = check(args.camera, args.threshold, args.telegram)
        print(report)
        sys.exit(0 if status == "healthy" else 1)


if __name__ == "__main__":
    main()
