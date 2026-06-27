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


async def init_db(sql_path: Path) -> None:
    if not sql_path.exists():
        logger.error(f"Schema file not found at {sql_path}")
        return
    async with engine.begin() as conn:
        raw = sql_path.read_text()
        stmt_count = 0
        for statement in raw.split(";"):
            stmt = statement.strip()
            if stmt:
                await conn.exec_driver_sql(stmt + ";")
                stmt_count += 1
        logger.info(f"Database initialized from {sql_path} ({stmt_count} statements)")
