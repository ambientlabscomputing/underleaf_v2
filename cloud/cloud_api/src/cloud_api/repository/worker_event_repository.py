"""Append-only worker timeline (`worker_events`)."""

from datetime import UTC, datetime

from sqlalchemy import select

from cloud_api.models.api.worker import WorkerEvent
from cloud_api.models.base import IDPrefix, generate_id
from cloud_api.models.sql import SQLWorkerEvent
from cloud_api.repository.base_repository import BaseRepository


def _to_event(row: SQLWorkerEvent) -> WorkerEvent:
    return WorkerEvent(
        id=row.id,
        worker_id=row.worker_id,
        task_id=row.task_id,
        connection_id=row.connection_id,
        type=row.type,
        message=row.message,
        data=row.data,
        created_at=row.created_at,
        updated_at=row.updated_at,
    )


class WorkerEventRepository(BaseRepository):
    async def append(
        self,
        type: str,
        message: str = "",
        *,
        worker_id: str | None = None,
        task_id: str | None = None,
        connection_id: str | None = None,
        data: dict | None = None,
    ) -> WorkerEvent:
        now = datetime.now(UTC)
        async with self.get_session() as session:
            row = SQLWorkerEvent(
                id=generate_id(IDPrefix.WORKER_EVENT),
                worker_id=worker_id,
                task_id=task_id,
                connection_id=connection_id,
                type=type,
                message=message,
                data=data or {},
                created_at=now,
                updated_at=now,
            )
            session.add(row)
            await session.commit()
            await session.refresh(row)
            return _to_event(row)

    async def list_events(
        self,
        *,
        worker_id: str | None = None,
        connection_id: str | None = None,
        task_id: str | None = None,
        types: list[str] | None = None,
        since: datetime | None = None,
        limit: int = 200,
    ) -> list[WorkerEvent]:
        """Oldest first, so it reads as a timeline."""
        async with self.get_session() as session:
            stmt = select(SQLWorkerEvent)
            if task_id is not None:
                stmt = stmt.where(SQLWorkerEvent.task_id == task_id)
            if types:
                stmt = stmt.where(SQLWorkerEvent.type.in_(types))
            if since is not None:
                stmt = stmt.where(SQLWorkerEvent.created_at >= since)
            if worker_id is not None:
                stmt = stmt.where(SQLWorkerEvent.worker_id == worker_id)
            if connection_id is not None:
                stmt = stmt.where(SQLWorkerEvent.connection_id == connection_id)
            rows = await session.scalars(
                stmt.order_by(SQLWorkerEvent.created_at.desc()).limit(limit)
            )
            return [_to_event(r) for r in reversed(rows.all())]
