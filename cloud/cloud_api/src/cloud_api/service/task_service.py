"""Runs the durable `worker_tasks` queue.

Today the only task is `create_connection`: a connection is recorded as
PENDING (slot reserved) and queued; once a conn_worker is ready the runner
creates it there under the same id and flips the connection to PROVISIONED.

Waiting for a worker is not a failure: the task goes back to the queue without
spending an attempt, bounded by `task_timeout_seconds`. Attempts are spent
only when a ready worker actually refuses or errors.
"""

import asyncio
from collections.abc import Awaitable, Callable
from datetime import UTC, datetime, timedelta

from cloud_api import logger
from cloud_api.config import WorkerDefaults
from cloud_api.lib.events import Event, EventBus
from cloud_api.models.api import Connection, ConnectionState, Status
from cloud_api.models.api.worker import WorkerTask, WorkerTaskKind, WorkerType
from cloud_api.repository.billing_account_repository import BillingAccountRepository
from cloud_api.repository.connection_repository import ConnectionRepository
from cloud_api.repository.worker_event_repository import WorkerEventRepository
from cloud_api.repository.worker_task_repository import WorkerTaskRepository
from cloud_api.service.entitlements_bucket_service import EntitlementsBucketService
from cloud_api.service.worker_service import (
    READY_TOPIC,
    WorkerService,
    WorkerUnavailableError,
)

TASK_TOPIC = "tasks.wake"


def connection_topic(connection_id: str) -> str:
    """Event-bus topic a connection's progress is published on (SSE wake-ups)."""
    return f"connections.{connection_id}"


class TaskService:
    def __init__(
        self,
        task_repo: WorkerTaskRepository,
        conn_repo: ConnectionRepository,
        billing_account_repo: BillingAccountRepository,
        entitlements_bucket_service: EntitlementsBucketService,
        worker_service: WorkerService,
        event_repo: WorkerEventRepository,
        event_bus: EventBus,
        settings: WorkerDefaults,
        refresh_settings: Callable[[], Awaitable[None]] | None = None,
    ):
        # Pulls operator overrides (worker_settings) into `settings` each tick.
        self.refresh_settings = refresh_settings
        self.task_repo = task_repo
        self.conn_repo = conn_repo
        self.billing_account_repo = billing_account_repo
        self.entitlements_bucket_service = entitlements_bucket_service
        self.worker_service = worker_service
        self.event_repo = event_repo
        self.event_bus = event_bus
        self.settings = settings

    # ------------------------------------------------------------------
    # Producing
    # ------------------------------------------------------------------

    async def enqueue_create_connection(self, connection: Connection) -> WorkerTask:
        """Queue the work and record the demand for a worker. Never blocks."""
        task = await self.task_repo.enqueue(
            WorkerTaskKind.CREATE_CONNECTION,
            connection_id=connection.id,
            payload={"node_id": connection.node_id, "name": connection.name},
            max_attempts=self.settings.task_max_attempts,
        )
        await self.event_repo.append(
            "task.queued",
            "Connection queued for a worker",
            task_id=task.id,
            connection_id=connection.id,
        )
        try:
            await self.worker_service.request_worker(WorkerType.CONNECTION_WORKER)
        except WorkerUnavailableError as exc:
            # Not fatal here: the runner reports it on the connection, and if
            # an operator fixes the cause in time the task still goes through.
            logger.bind(connection_id=connection.id).warning(
                f"no worker requested: {exc}"
            )
        await self.event_bus.publish(Event(TASK_TOPIC))
        return task

    async def cancel_for_connection(self, connection_id: str, reason: str) -> int:
        return await self.task_repo.cancel_for_connection(connection_id, reason)

    async def has_pending_work(self) -> bool:
        return await self.task_repo.has_active()

    # ------------------------------------------------------------------
    # Consuming
    # ------------------------------------------------------------------

    async def run_once(self, batch: int = 10) -> int:
        """Claim and process due tasks; returns how many were processed."""
        lease = self.settings.task_poll_seconds * 6 + 60  # comfortably > one attempt
        tasks = await self.task_repo.claim(batch, lease_seconds=lease)
        # Concurrent: one slow gRPC call mustn't hold up the others.
        await asyncio.gather(*(self._process_safely(t) for t in tasks))
        return len(tasks)

    async def run_loop(self) -> None:
        """Process until cancelled. Events (new task, worker ready) only cut
        the wait short; the queue itself is re-read every poll interval."""
        poll = self.settings.task_poll_seconds
        with (
            self.event_bus.subscription(TASK_TOPIC) as task_sub,
            self.event_bus.subscription(READY_TOPIC) as ready_sub,
        ):
            while True:
                try:
                    if self.refresh_settings is not None:
                        await self.refresh_settings()
                    processed = await self.run_once()
                except asyncio.CancelledError:
                    raise
                except Exception as exc:  # noqa: BLE001 -- keep the loop alive
                    logger.error(f"task runner tick failed: {exc}")
                    processed = 0
                await _wait_any([task_sub, ready_sub], poll if not processed else 0.5)

    async def _process_safely(self, task: WorkerTask) -> None:
        with logger.contextualize(task_id=task.id, connection_id=task.connection_id):
            try:
                await self._process(task)
            except asyncio.CancelledError:
                raise
            except Exception as exc:  # noqa: BLE001
                logger.exception(f"task {task.id} crashed")
                await self._attempt_failed(task, f"internal error: {exc}")

    async def _process(self, task: WorkerTask) -> None:
        if task.kind != WorkerTaskKind.CREATE_CONNECTION or task.connection_id is None:
            await self.task_repo.fail(task.id, f"unsupported task kind {task.kind}")
            return

        conn = await self.conn_repo.get_connection(task.connection_id)
        if (
            conn is None
            or conn.state != ConnectionState.PENDING
            or conn.status != Status.IN_PROGRESS
        ):
            await self.task_repo.cancel(task.id, "connection is no longer pending")
            return

        age = (datetime.now(UTC) - task.created_at).total_seconds()
        if age > self.settings.task_timeout_seconds:
            await self._give_up(
                task,
                conn,
                f"no worker became ready within {self.settings.task_timeout_seconds}s",
            )
            return

        worker = await self.worker_service.get_ready_worker(
            WorkerType.CONNECTION_WORKER
        )
        if worker is None:
            await self._wait_for_worker(task, conn)
            return

        try:
            client = await self.worker_service.get_client(worker)
            created = await client.new_connection(conn.node_id, conn.name, id=conn.id)
        except Exception as exc:  # noqa: BLE001 -- gRPC, TLS, worker going away...
            await self._attempt_failed(task, f"{type(exc).__name__}: {exc}", conn)
            return

        if await self.conn_repo.activate_pending(conn.id, created):
            await self.task_repo.succeed(task.id, worker_id=worker.id)
            await self.event_repo.append(
                "task.succeeded",
                "Connection created on worker",
                task_id=task.id,
                connection_id=conn.id,
                worker_id=worker.id,
            )
            await self._notify(conn.id)
            return

        # Terminated (or failed) while we were creating it: don't leave an
        # orphan on the worker.
        try:
            await client.terminate_connection(conn.id)
        except Exception as exc:  # noqa: BLE001
            logger.warning(f"could not remove superseded connection from worker: {exc}")
        await self.task_repo.cancel(task.id, "connection ended while being created")

    # ------------------------------------------------------------------
    # Outcomes
    # ------------------------------------------------------------------

    async def _wait_for_worker(self, task: WorkerTask, conn: Connection) -> None:
        try:
            # Idempotent: also revives a worker that went away since enqueue.
            await self.worker_service.request_worker(WorkerType.CONNECTION_WORKER)
        except WorkerUnavailableError as exc:
            # Waiting won't help (provisioning disabled, breaker open, ...).
            await self._give_up(task, conn, f"cannot start a worker: {exc}")
            return
        if await self.conn_repo.set_pending_detail(
            conn.id, "waiting for a worker to start"
        ):
            await self._notify(conn.id)
        await self.task_repo.release(
            task.id,
            datetime.now(UTC) + timedelta(seconds=self.settings.task_poll_seconds),
        )

    async def _attempt_failed(
        self, task: WorkerTask, error: str, conn: Connection | None = None
    ) -> None:
        spent = task.attempts + 1
        await self.event_repo.append(
            "task.attempt_failed",
            error,
            task_id=task.id,
            connection_id=task.connection_id,
            data={"attempt": spent, "max_attempts": task.max_attempts},
        )
        if spent >= task.max_attempts:
            if conn is None and task.connection_id:
                conn = await self.conn_repo.get_connection(task.connection_id)
            if conn is not None:
                await self._give_up(
                    task,
                    conn,
                    f"gave up after {spent} attempts: {error}",
                    attempts=spent,
                )
            else:
                await self.task_repo.fail(task.id, error, attempts=spent)
            return
        backoff = self.settings.task_retry_backoff_seconds * 2 ** (spent - 1)
        await self.task_repo.retry(
            task.id, datetime.now(UTC) + timedelta(seconds=backoff), error
        )

    async def _give_up(
        self,
        task: WorkerTask,
        conn: Connection,
        reason: str,
        attempts: int | None = None,
    ) -> None:
        """Fail the connection and hand its slot back -- exactly once, even if
        a user terminated it at the same moment."""
        await self.task_repo.fail(task.id, reason, attempts=attempts)
        if await self.conn_repo.fail_pending(conn.id, reason):
            account = await self.billing_account_repo.get_billing_account_by_node_id(
                conn.node_id
            )
            if account is not None:
                await self.entitlements_bucket_service.release_connection_slot(
                    account.id
                )
        await self.event_repo.append(
            "task.failed", reason, task_id=task.id, connection_id=conn.id
        )
        logger.bind(connection_id=conn.id).warning(f"Connection failed: {reason}")
        await self._notify(conn.id)

    async def _notify(self, connection_id: str) -> None:
        await self.event_bus.publish(Event(connection_topic(connection_id)))


async def _wait_any(subs: list, timeout: float) -> None:
    """Return when any subscription has an event, or after `timeout`."""
    waiters = [asyncio.ensure_future(s.get(timeout=timeout)) for s in subs]
    try:
        await asyncio.wait(waiters, return_when=asyncio.FIRST_COMPLETED)
    finally:
        for w in waiters:
            w.cancel()
