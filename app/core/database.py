"""Async SQLAlchemy engine, session factory, and schema initialization.

Creates the shared async engine and session maker from application settings
and provides ``init_db`` to bootstrap the SQLite schema from a raw SQL file
on application startup.
"""

from pathlib import Path

from loguru import logger
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine

from app.core.config import settings

engine = create_async_engine(
    settings.database_url,
    echo=settings.debug,
    connect_args={"check_same_thread": False},
)

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
        raw = sql_path.read_text()
        stmt_count = 0
        for statement in raw.split(";"):
            stmt = statement.strip()
            if stmt:
                await conn.exec_driver_sql(stmt + ";")
                stmt_count += 1
        logger.info(f"Database initialized from {sql_path} ({stmt_count} statements)")
