from sqlalchemy.ext.asyncio import (
    AsyncEngine,
    AsyncSession,
    async_sessionmaker,
    create_async_engine,
)

from cloud_api import AppConfig

_engine: AsyncEngine | None = None
_sessionmaker: async_sessionmaker[AsyncSession] | None = None


def init_session_maker(cfg: AppConfig) -> None:
    global _engine, _sessionmaker
    _engine = create_async_engine(cfg.db.async_connection_string, echo=cfg.db.log_sql)
    _sessionmaker = async_sessionmaker(_engine, expire_on_commit=False)


def get_engine() -> AsyncEngine:
    if _engine is None:
        raise RuntimeError(
            "Session maker not initialized. Call init_session_maker() first."
        )
    return _engine


def get_session() -> AsyncSession:
    if _sessionmaker is None:
        raise RuntimeError(
            "Session maker not initialized. Call init_session_maker() first."
        )
    return _sessionmaker()
