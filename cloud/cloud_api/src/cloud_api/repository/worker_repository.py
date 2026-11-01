"""Persistence for conn_worker VMs (the `workers` table)."""

from datetime import UTC, datetime

from sqlalchemy import and_, delete, func, insert, literal, not_, select, update
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.exc import IntegrityError

from cloud_api.models.api.common import Status
from cloud_api.models.api.worker import (
    QueryWorkerRequest,
    Worker,
    WorkerConfig,
    WorkerState,
    WorkerType,
)
from cloud_api.models.sql import SQLWorker
from cloud_api.repository.base_repository import BaseRepository


def _apply_extra_patch(update_data: dict) -> None:
    """`extra_patch={...}` becomes an atomic JSONB merge (null values delete
    keys), so concurrent writers touching different keys can't lose updates."""
    patch = update_data.pop("extra_patch", None)
    if patch is not None:
        update_data["extra_data"] = func.jsonb_strip_nulls(
            SQLWorker.extra_data.op("||")(literal(patch, JSONB))
        )


def _is_live():
    """SQL twin of `Worker.is_live`: everything but DELETED + SUCCEEDED."""
    return not_(
        and_(
            SQLWorker.state == WorkerState.DELETED.value,
            SQLWorker.status == Status.SUCCEEDED.value,
        )
    )


def _to_worker(row: SQLWorker) -> Worker:
    return Worker(
        id=row.id,
        worker_type=WorkerType(row.worker_type),
        config=WorkerConfig.model_validate(row.config),
        state=WorkerState(row.state),
        status=Status(row.status),
        extra_data=row.extra_data,
        error=row.error,
        state_changed_at=row.state_changed_at,
        created_at=row.created_at,
        updated_at=row.updated_at,
    )


class WorkerRepository(BaseRepository):
    async def create_worker(self, worker: Worker) -> Worker:
        async with self.get_session() as session:
            now = datetime.now(UTC)
            stmt = (
                insert(SQLWorker)
                .values(
                    id=worker.id,
                    worker_type=worker.worker_type.value,
                    state=worker.state.value,
                    status=worker.status.value,
                    error=worker.error,
                    extra_data=worker.extra_data,
                    config=worker.config.model_dump(mode="json"),
                    state_changed_at=now,
                    created_at=now,
                    updated_at=now,
                )
                .returning(SQLWorker)
            )
            row = (await session.execute(stmt)).scalar_one()
            await session.commit()
            return _to_worker(row)

    async def get_or_create_live_worker(self, worker: Worker) -> tuple[Worker, bool]:
        """Insert `worker`, or return the type's existing live worker.

        The partial unique index on `workers` allows one live worker
        per type, so concurrent callers race on the insert and exactly one
        wins; the losers read the winner. Returns (worker, created).
        """
        try:
            return await self.create_worker(worker), True
        except IntegrityError:
            existing = await self.get_live_worker(worker.worker_type)
            if existing is None:
                # The live worker was deleted between our failed insert and
                # the read; the caller can simply retry.
                raise
            return existing, False

    async def get_worker(self, worker_id: str) -> Worker | None:
        async with self.get_session() as session:
            row = await session.get(SQLWorker, worker_id)
            return _to_worker(row) if row is not None else None

    async def get_live_worker(self, worker_type: WorkerType) -> Worker | None:
        async with self.get_session() as session:
            row = await session.scalar(
                select(SQLWorker)
                .where(SQLWorker.worker_type == worker_type.value)
                .where(_is_live())
            )
            return _to_worker(row) if row is not None else None

    async def list_workers(self, query: QueryWorkerRequest) -> tuple[int, list[Worker]]:
        async with self.get_session() as session:
            filters = []
            if query.worker_type is not None:
                filters.append(SQLWorker.worker_type == query.worker_type.value)
            if query.state is not None:
                filters.append(SQLWorker.state == query.state.value)
            if query.status is not None:
                filters.append(SQLWorker.status == query.status.value)
            for key, value in (query.model_extra or {}).items():
                filters.append(SQLWorker.extra_data[key].astext == str(value))

            # total count ignores pagination and ordering
            total_count = (
                await session.execute(
                    select(func.count()).select_from(SQLWorker).where(*filters)
                )
            ).scalar_one()

            stmt = select(SQLWorker).where(*filters)
            order_column = getattr(SQLWorker, query.order_by, None)
            if order_column is not None:
                if query.order_direction.lower() == "desc":
                    stmt = stmt.order_by(order_column.desc())
                else:
                    stmt = stmt.order_by(order_column.asc())
            stmt = stmt.offset(query.offset).limit(query.limit)

            rows = (await session.execute(stmt)).scalars().all()
            return total_count, [_to_worker(row) for row in rows]

    async def delete_worker(self, worker_id: str) -> None:
        async with self.get_session() as session:
            await session.execute(delete(SQLWorker).where(SQLWorker.id == worker_id))
            await session.commit()

    async def list_live_workers(self) -> list[Worker]:
        async with self.get_session() as session:
            rows = await session.scalars(
                select(SQLWorker).where(_is_live()).order_by(SQLWorker.created_at)
            )
            return [_to_worker(row) for row in rows.all()]

    async def list_active_since(self, since: datetime) -> list[Worker]:
        """Workers that existed at any point since `since`: still live, or
        created/deleted in the window. What a cost rollup needs."""
        async with self.get_session() as session:
            rows = await session.scalars(
                select(SQLWorker)
                .where(_is_live() | (SQLWorker.state_changed_at >= since))
                .order_by(SQLWorker.created_at)
            )
            return [_to_worker(row) for row in rows.all()]

    async def count_failed_since(self, worker_type: WorkerType, since: datetime) -> int:
        """Workers of this type that were torn down after failing (they carry
        an `error`) since `since` -- feeds the provisioning circuit breaker."""
        async with self.get_session() as session:
            return (
                await session.execute(
                    select(func.count())
                    .select_from(SQLWorker)
                    .where(SQLWorker.worker_type == worker_type.value)
                    .where(SQLWorker.state == WorkerState.DELETED.value)
                    .where(SQLWorker.status == Status.SUCCEEDED.value)
                    .where(SQLWorker.error.is_not(None))
                    .where(SQLWorker.state_changed_at >= since)
                )
            ).scalar_one()

    async def transition(
        self,
        worker_id: str,
        expect: tuple[WorkerState, Status],
        **update_data,
    ) -> Worker | None:
        """Compare-and-set: apply `update_data` only if the worker is still at
        `expect` (state, status). Returns the new worker, or None if someone
        else moved it first -- the guard that lets the reconciler, the VM
        callbacks and the admin CLI all write without clobbering each other."""
        now = datetime.now(UTC)
        update_data.setdefault("updated_at", now)
        if "state" in update_data or "status" in update_data:
            update_data.setdefault("state_changed_at", now)
        _apply_extra_patch(update_data)
        async with self.get_session() as session:
            stmt = (
                update(SQLWorker)
                .where(SQLWorker.id == worker_id)
                .where(SQLWorker.state == expect[0].value)
                .where(SQLWorker.status == expect[1].value)
                .values(**update_data)
                .returning(SQLWorker)
            )
            row = (await session.execute(stmt)).scalar_one_or_none()
            await session.commit()
            return _to_worker(row) if row is not None else None

    async def update_worker(self, worker_id: str, **update_data) -> Worker | None:
        now = datetime.now(UTC)
        update_data.setdefault("updated_at", now)
        if "state" in update_data or "status" in update_data:
            update_data.setdefault("state_changed_at", now)
        _apply_extra_patch(update_data)
        async with self.get_session() as session:
            stmt = (
                update(SQLWorker)
                .where(SQLWorker.id == worker_id)
                .values(**update_data)
                .returning(SQLWorker)
            )
            row = (await session.execute(stmt)).scalar_one_or_none()
            await session.commit()
            return _to_worker(row) if row is not None else None
