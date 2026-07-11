"""Async SQLAlchemy engine, session factory, and schema initialization.

Creates the shared async engine and session maker from application settings
and provides ``init_db`` to bootstrap the SQLite schema from a raw SQL file
on application startup.
"""

from pathlib import Path

from loguru import logger
from sqlalchemy import event
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine

from app.core.config import settings

engine = create_async_engine(
    settings.database_url,
    echo=settings.debug,
    connect_args={"check_same_thread": False},
)


@event.listens_for(engine.sync_engine, "connect")
def _set_sqlite_pragma(dbapi_connection, connection_record):
    """Enable WAL mode and busy timeout for concurrent access."""
    cursor = dbapi_connection.cursor()
    cursor.execute("PRAGMA journal_mode=WAL")
    cursor.execute("PRAGMA busy_timeout=5000")
    cursor.close()

session_factory = async_sessionmaker(
    engine,
    class_=AsyncSession,
    expire_on_commit=False,
)


async def _migrate_interval_column(conn) -> None:
    """Migrate legacy interval_minutes column to interval_seconds."""
    result = await conn.exec_driver_sql("PRAGMA table_info(cameras)")
    columns = {row["name"] for row in result.mappings().all()}
    if "interval_minutes" not in columns:
        return
    logger.warning("Legacy interval_minutes column found; migrating to interval_seconds")
    await conn.exec_driver_sql(
        "ALTER TABLE cameras RENAME COLUMN interval_minutes TO interval_seconds"
    )
    await conn.exec_driver_sql(
        "UPDATE cameras SET interval_seconds = interval_seconds * 60"
    )
    logger.info("Migration complete: interval_minutes -> interval_seconds")


async def _migrate_archive_column(conn) -> None:
    """Add archive_path column to snapshots table if missing."""
    result = await conn.exec_driver_sql("PRAGMA table_info(snapshots)")
    columns = {row["name"] for row in result.mappings().all()}
    if "archive_path" in columns:
        return
    logger.warning("archive_path column missing; adding to snapshots table")
    await conn.exec_driver_sql("ALTER TABLE snapshots ADD COLUMN archive_path TEXT")
    logger.info("Migration complete: added archive_path to snapshots")


async def _migrate_analysis_tables(conn) -> None:
    """Create analysis_jobs and snapshot_analyses tables if missing."""
    result = await conn.exec_driver_sql("SELECT name FROM sqlite_master WHERE type='table'")
    tables = {row["name"] for row in result.mappings().all()}
    if "analysis_jobs" not in tables:
        logger.warning("analysis_jobs table missing; creating it")
        await conn.exec_driver_sql("""
            CREATE TABLE IF NOT EXISTS analysis_jobs (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                snapshot_id INTEGER NOT NULL,
                job_type TEXT NOT NULL DEFAULT 'yolo_detection',
                status TEXT NOT NULL DEFAULT 'pending',
                priority INTEGER NOT NULL DEFAULT 0,
                attempts INTEGER NOT NULL DEFAULT 0,
                max_attempts INTEGER NOT NULL DEFAULT 3,
                error_message TEXT,
                requested_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                started_at TIMESTAMP,
                finished_at TIMESTAMP,
                FOREIGN KEY (snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE
            )
        """)
        await conn.exec_driver_sql("CREATE INDEX IF NOT EXISTS idx_analysis_jobs_status ON analysis_jobs(status, priority)")
        await conn.exec_driver_sql("CREATE INDEX IF NOT EXISTS idx_analysis_jobs_snapshot ON analysis_jobs(snapshot_id)")
        logger.info("Migration complete: created analysis_jobs table")
    if "snapshot_analyses" not in tables:
        logger.warning("snapshot_analyses table missing; creating it")
        await conn.exec_driver_sql("""
            CREATE TABLE IF NOT EXISTS snapshot_analyses (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                snapshot_id INTEGER NOT NULL,
                model_name TEXT NOT NULL,
                model_version TEXT NOT NULL DEFAULT '1.0',
                status TEXT NOT NULL DEFAULT 'pending',
                objects_json TEXT,
                person_count INTEGER NOT NULL DEFAULT 0,
                review_required BOOLEAN NOT NULL DEFAULT 0,
                review_reason TEXT,
                anomaly_score REAL,
                error_message TEXT,
                analyzed_at TIMESTAMP,
                created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                FOREIGN KEY (snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE,
                UNIQUE(snapshot_id, model_name)
            )
        """)
        await conn.exec_driver_sql("CREATE INDEX IF NOT EXISTS idx_analyses_review ON snapshot_analyses(review_required, status)")
        logger.info("Migration complete: created snapshot_analyses table")


async def init_db(sql_path: Path) -> None:
    """Initialize the database schema from a raw SQL file.

    Reads the SQL file, splits it into statements by semicolon, and executes
    each non-empty statement against the database engine within a transaction.

    Args:
        sql_path: Path to the SQL DDL file to execute.
    """
    if not sql_path.exists():
        logger.error(f"Schema file not found at {sql_path}")
        return
    async with engine.begin() as conn:
        await _migrate_interval_column(conn)
        await _migrate_archive_column(conn)
        await _migrate_analysis_tables(conn)
        raw = sql_path.read_text()
        stmt_count = 0
        for statement in raw.split(";"):
            stmt = statement.strip()
            if stmt:
                await conn.exec_driver_sql(stmt + ";")
                stmt_count += 1
        logger.info(f"Database initialized from {sql_path} ({stmt_count} statements)")
