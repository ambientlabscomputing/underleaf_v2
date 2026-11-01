"""The durable queue (`worker_tasks`).

Tasks are claimed with FOR UPDATE SKIP LOCKED plus a lease, so concurrent
runners never take the same row and a crashed runner's tasks become claimable
again once the lease lapses.
"""

from datetime import UTC, datetime, timedelta

from sqlalchemy import func, or_, select, update

from cloud_api.models.api.worker import WorkerTask, WorkerTaskKind, WorkerTaskState
from cloud_api.models.base import IDPrefix, generate_id
from cloud_api.models.sql import SQLWorkerTask
from cloud_api.repository.base_repository import BaseRepository

ACTIVE_STATES = (WorkerTaskState.QUEUED.value, WorkerTaskState.RUNNING.value)


def _to_task(row: SQLWorkerTask) -> WorkerTask:
    return WorkerTask(
        id=row.id,
        kind=WorkerTaskKind(row.kind),
        state=WorkerTaskState(row.state),
        payload=row.payload or {},
        connection_id=row.connection_id,
        worker_id=row.worker_id,
        attempts=row.attempts,
        max_attempts=row.max_attempts,
        run_after=row.run_after,
        lease_expires_at=row.lease_expires_at,
        error=row.error,
        started_at=row.started_at,
        finished_at=row.finished_at,
        created_at=row.created_at,
        updated_at=row.updated_at,
    )


class WorkerTaskRepository(BaseRepository):
    async def enqueue(
        self,
        kind: WorkerTaskKind,
        *,
        connection_id: str | None = None,
        payload: dict | None = None,
        max_attempts: int = 5,
        run_after: datetime | None = None,
    ) -> WorkerTask:
        now = datetime.now(UTC)
        async with self.get_session() as session:
            row = SQLWorkerTask(
                id=generate_id(IDPrefix.WORKER_TASK),
                kind=kind.value,
                state=WorkerTaskState.QUEUED.value,
                payload=payload or {},
                connection_id=connection_id,
                max_attempts=max_attempts,
                run_after=run_after or now,
                created_at=now,
                updated_at=now,
            )
            session.add(row)
            await session.commit()
            await session.refresh(row)
            return _to_task(row)

    async def get(self, task_id: str) -> WorkerTask | None:
        async with self.get_session() as session:
            row = await session.get(SQLWorkerTask, task_id)
            return _to_task(row) if row else None

    async def get_for_connection(self, connection_id: str) -> WorkerTask | None:
        async with self.get_session() as session:
            row = await session.scalar(
                select(SQLWorkerTask)
                .where(SQLWorkerTask.connection_id == connection_id)
                .order_by(SQLWorkerTask.created_at.desc())
                .limit(1)
            )
            return _to_task(row) if row else None

    async def list_tasks(
        self, state: WorkerTaskState | None = None, limit: int = 100
    ) -> list[WorkerTask]:
        async with self.get_session() as session:
            stmt = select(SQLWorkerTask).order_by(SQLWorkerTask.created_at.desc())
            if state is not None:
                stmt = stmt.where(SQLWorkerTask.state == state.value)
            rows = await session.scalars(stmt.limit(limit))
            return [_to_task(r) for r in rows]

    async def claim(self, limit: int, lease_seconds: float) -> list[WorkerTask]:
        """Take up to `limit` runnable tasks: due queued ones, plus running
        ones whose lease lapsed (a crashed runner). A reclaimed task counts the
        lost attempt so a task that keeps killing its runner can't loop forever."""
        now = datetime.now(UTC)
        async with self.get_session() as session:
            rows = (
                await session.scalars(
                    select(SQLWorkerTask)
                    .where(
                        or_(
                            (SQLWorkerTask.state == WorkerTaskState.QUEUED.value)
                            & (SQLWorkerTask.run_after <= now),
                            (SQLWorkerTask.state == WorkerTaskState.RUNNING.value)
                            & (SQLWorkerTask.lease_expires_at < now),
                        )
                    )
                    .order_by(SQLWorkerTask.run_after)
                    .limit(limit)
                    .with_for_update(skip_locked=True)
                )
            ).all()
            for row in rows:
                if row.state == WorkerTaskState.RUNNING.value:
                    row.attempts += 1
                row.state = WorkerTaskState.RUNNING.value
                row.lease_expires_at = now + timedelta(seconds=lease_seconds)
                row.started_at = row.started_at or now
                row.updated_at = now
            await session.commit()
            return [_to_task(r) for r in rows]

    async def _finish(
        self,
        task_id: str,
        state: WorkerTaskState,
        *,
        error: str | None = None,
        worker_id: str | None = None,
        attempts: int | None = None,
    ) -> bool:
        now = datetime.now(UTC)
        values: dict = {
            "state": state.value,
            "finished_at": now,
            "updated_at": now,
            "lease_expires_at": None,
            "error": error,
        }
        if worker_id is not None:
            values["worker_id"] = worker_id
        if attempts is not None:
            values["attempts"] = attempts
        async with self.get_session() as session:
            result = await session.execute(
                update(SQLWorkerTask)
                .where(SQLWorkerTask.id == task_id)
                .where(SQLWorkerTask.state.in_(ACTIVE_STATES))
                .values(**values)
            )
            await session.commit()
            return result.rowcount > 0

    async def succeed(self, task_id: str, worker_id: str | None = None) -> bool:
        return await self._finish(
            task_id, WorkerTaskState.SUCCEEDED, worker_id=worker_id
        )

    async def fail(self, task_id: str, error: str, attempts: int | None = None) -> bool:
        """`attempts` records the final (failed) attempt, which retry() never
        got the chance to count."""
        return await self._finish(
            task_id, WorkerTaskState.FAILED, error=error, attempts=attempts
        )

    async def cancel(self, task_id: str, reason: str = "cancelled") -> bool:
        return await self._finish(task_id, WorkerTaskState.CANCELLED, error=reason)

    async def cancel_for_connection(self, connection_id: str, reason: str) -> int:
        now = datetime.now(UTC)
        async with self.get_session() as session:
            result = await session.execute(
                update(SQLWorkerTask)
                .where(SQLWorkerTask.connection_id == connection_id)
                .where(SQLWorkerTask.state.in_(ACTIVE_STATES))
                .values(
                    state=WorkerTaskState.CANCELLED.value,
                    error=reason,
                    finished_at=now,
                    updated_at=now,
                    lease_expires_at=None,
                )
            )
            await session.commit()
            return result.rowcount

    async def release(self, task_id: str, run_after: datetime) -> None:
        """Back to the queue without spending an attempt (nothing was tried;
        the task is just waiting on a worker)."""
        await self._requeue(task_id, run_after, error=None, spend_attempt=False)

    async def retry(self, task_id: str, run_after: datetime, error: str) -> None:
        """Back to the queue after a failed attempt."""
        await self._requeue(task_id, run_after, error=error, spend_attempt=True)

    async def _requeue(
        self,
        task_id: str,
        run_after: datetime,
        *,
        error: str | None,
        spend_attempt: bool,
    ) -> None:
        values: dict = {
            "state": WorkerTaskState.QUEUED.value,
            "run_after": run_after,
            "lease_expires_at": None,
            "updated_at": datetime.now(UTC),
        }
        if error is not None:
            values["error"] = error
        if spend_attempt:
            values["attempts"] = SQLWorkerTask.attempts + 1
        async with self.get_session() as session:
            await session.execute(
                update(SQLWorkerTask)
                .where(SQLWorkerTask.id == task_id)
                .where(SQLWorkerTask.state == WorkerTaskState.RUNNING.value)
                .values(**values)
            )
            await session.commit()

    async def count_active(self) -> int:
        async with self.get_session() as session:
            return int(
                await session.scalar(
                    select(func.count(SQLWorkerTask.id)).where(
                        SQLWorkerTask.state.in_(ACTIVE_STATES)
                    )
                )
                or 0
            )

    async def has_active(self) -> bool:
        """Queued or running work: keeps a worker from being idled out."""
        return await self.count_active() > 0

    async def stats(self) -> dict:
        """Tasks per state, and how long the oldest still-queued one has waited."""
        async with self.get_session() as session:
            counts = dict(
                (
                    await session.execute(
                        select(
                            SQLWorkerTask.state, func.count(SQLWorkerTask.id)
                        ).group_by(SQLWorkerTask.state)
                    )
                ).all()
            )
            oldest = await session.scalar(
                select(func.min(SQLWorkerTask.created_at)).where(
                    SQLWorkerTask.state == WorkerTaskState.QUEUED.value
                )
            )
        return {
            "by_state": {s.value: int(counts.get(s.value, 0)) for s in WorkerTaskState},
            "oldest_queued_age_seconds": (
                (datetime.now(UTC) - oldest).total_seconds() if oldest else None
            ),
        }
