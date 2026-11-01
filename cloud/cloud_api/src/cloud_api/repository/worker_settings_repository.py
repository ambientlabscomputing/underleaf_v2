"""Operator overrides of worker settings (`worker_settings`)."""

from datetime import UTC, datetime

from sqlalchemy import delete, select
from sqlalchemy.dialects.postgresql import insert

from cloud_api.models.sql import SQLWorkerSetting
from cloud_api.repository.base_repository import BaseRepository


class WorkerSettingsRepository(BaseRepository):
    async def get_all(self) -> dict[str, object]:
        async with self.get_session() as session:
            rows = await session.scalars(select(SQLWorkerSetting))
            return {r.id: r.value["v"] for r in rows}

    async def set(self, key: str, value: object) -> None:
        now = datetime.now(UTC)
        stmt = insert(SQLWorkerSetting).values(
            id=key, value={"v": value}, created_at=now, updated_at=now
        )
        async with self.get_session() as session:
            await session.execute(
                stmt.on_conflict_do_update(
                    index_elements=["id"],
                    set_={"value": stmt.excluded.value, "updated_at": now},
                )
            )
            await session.commit()

    async def unset(self, key: str) -> None:
        async with self.get_session() as session:
            await session.execute(
                delete(SQLWorkerSetting).where(SQLWorkerSetting.id == key)
            )
            await session.commit()
