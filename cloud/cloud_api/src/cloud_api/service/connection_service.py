import asyncio
from collections.abc import AsyncIterator
from datetime import UTC, datetime
from itertools import batched

from async_lru import alru_cache

from cloud_api import logger
from cloud_api.lib.conn_worker_client.conn_worker_client import ConnWorkerClient
from cloud_api.lib.events import EventBus
from cloud_api.models.api import (
    BillingAccount,
    Connection,
    ConnectionState,
    CreateConnectionRequest,
    ListConnectionResponse,
    PatchConnectionRequest,
    QueryConnectionRequest,
    Status,
)
from cloud_api.repository.billing_account_repository import BillingAccountRepository
from cloud_api.repository.connection_repository import ConnectionRepository
from cloud_api.service.entitlements_bucket_service import (
    EntitlementsBucketService,
    EntitlementsExceededError,
)
from cloud_api.service.task_service import TaskService, connection_topic


class ConnectionNotFoundError(Exception):
    def __init__(self, connection_id: str) -> None:
        super().__init__(f"Connection '{connection_id}' not found")
        self.connection_id = connection_id


class ConnectionStateError(Exception):
    """The operation isn't valid for the connection's current state."""


class ConnectionService:
    def __init__(
        self,
        conn_repo: ConnectionRepository,
        conn_client: ConnWorkerClient,
        billing_account_repo: BillingAccountRepository,
        entitlements_bucket_service: EntitlementsBucketService,
        task_service: TaskService | None = None,
        event_bus: EventBus | None = None,
    ) -> None:
        # With a task_service, connections are created asynchronously: recorded
        # as PENDING and queued until a worker VM is up. Without one (a static
        # conn_worker) they are created on the spot.
        self.task_service = task_service
        self.event_bus = event_bus
        self.conn_repo = conn_repo
        self.conn_client = conn_client
        self.billing_account_repo = billing_account_repo
        self.entitlements_bucket_service = entitlements_bucket_service

    async def get_owner_principal_id(self, connection_id: str) -> str | None:
        return await self.conn_repo.get_principal_account_id(connection_id)

    async def get_node_owner_principal_id(self, node_id: str) -> str | None:
        return await self.conn_repo.get_node_principal_account_id(node_id)

    async def _billing_account_for_node(self, node_id: str) -> BillingAccount | None:
        return await self.billing_account_repo.get_billing_account_by_node_id(node_id)

    async def _reserve_slot(self, account: BillingAccount) -> None:
        """Reserve a connection slot, healing leaked slots once if full.

        The reserve/release counter can drift (a connection dropped without
        going through terminate_connection). When the counter says "full" we
        therefore re-derive it from the connections actually open and retry
        once -- only at the limit, so the common path stays a single atomic
        decrement and concurrent creates can't over-reserve.
        """
        try:
            await self.entitlements_bucket_service.check_and_reserve_connection_slot(
                account.id
            )
            return
        except EntitlementsExceededError:
            try:
                await self._sync_connections()
                open_count = await self.conn_repo.count_open_for_principal(
                    account.principal_account_id
                )
                await self.entitlements_bucket_service.reconcile_connection_slots(
                    account.id, open_count
                )
            except Exception as exc:  # noqa: BLE001
                logger.bind(billing_account_id=account.id).warning(
                    f"Slot reconciliation failed: {exc}"
                )
        await self.entitlements_bucket_service.check_and_reserve_connection_slot(
            account.id
        )

    async def create_connection(self, req: CreateConnectionRequest) -> Connection:
        account = await self._billing_account_for_node(req.node_id)
        if account is not None:
            await self._reserve_slot(account)
        try:
            if self.task_service is not None:
                return await self._queue_connection(req)
            connection = await self.conn_client.new_connection(
                node_id=req.node_id, name=req.name
            )
            connection = await self.conn_repo.create_connection(connection)
        except Exception:
            # The slot was reserved before we knew the connection would exist.
            if account is not None:
                await self.entitlements_bucket_service.release_connection_slot(
                    account.id
                )
            raise
        logger.bind(connection_id=connection.id).info("Connection created")
        return connection

    async def _queue_connection(self, req: CreateConnectionRequest) -> Connection:
        """Record the connection as PENDING and queue its creation. The slot
        reserved by the caller is held until it is provisioned, fails or is
        terminated."""
        assert self.task_service is not None
        connection = await self.conn_repo.create_connection(
            Connection(
                name=req.name,
                node_id=req.node_id,
                state=ConnectionState.PENDING,
                status=Status.IN_PROGRESS,
                status_detail="queued",
            )
        )
        try:
            await self.task_service.enqueue_create_connection(connection)
        except Exception:
            # Never leave a PENDING row nothing will ever process.
            await self.conn_repo.delete_connection(connection.id)
            raise
        logger.bind(connection_id=connection.id).info("Connection queued")
        return connection

    async def watch_connection(
        self,
        connection_id: str,
        poll_seconds: float = 2.0,
        heartbeat_seconds: float = 15.0,
    ) -> AsyncIterator[Connection | None]:
        """Yield the connection whenever its state, status or detail changes,
        until it is no longer waiting on a worker. Yields None as a keep-alive
        while idle. Events wake it early; the DB is re-read every
        `poll_seconds` regardless, so a missed event only costs latency."""
        loop = asyncio.get_running_loop()
        bus = self.event_bus
        sub = bus.subscription(connection_topic(connection_id)) if bus else None
        try:
            last: tuple | None = None
            last_beat = loop.time()
            while True:
                connection = await self.conn_repo.get_connection(connection_id)
                if connection is None:
                    return
                key = (connection.state, connection.status, connection.status_detail)
                if key != last:
                    last = key
                    last_beat = loop.time()
                    yield connection
                if (
                    connection.state != ConnectionState.PENDING
                    or connection.status == Status.FAILED
                ):
                    return
                if loop.time() - last_beat >= heartbeat_seconds:
                    last_beat = loop.time()
                    yield None
                if sub is not None:
                    await sub.get(timeout=poll_seconds)
                else:
                    await asyncio.sleep(poll_seconds)
        finally:
            if sub is not None:
                sub.close()

    async def get_connections(
        self, req: QueryConnectionRequest
    ) -> ListConnectionResponse:
        await self.wait_for_sync()
        items = await self.conn_repo.query_connections(req)
        return ListConnectionResponse(items=items, total=len(items))

    async def get_connection(self, connection_id: str) -> Connection:
        await self.wait_for_sync()
        connection = await self.conn_repo.get_connection(connection_id)
        if connection is None:
            raise ConnectionNotFoundError(connection_id)
        return connection

    async def patch_connection(
        self, connection_id: str, req: PatchConnectionRequest
    ) -> Connection:
        connection = await self.conn_repo.patch_connection(connection_id, req)
        if connection is None:
            raise ConnectionNotFoundError(connection_id)
        await self.sync_connections()
        return connection

    async def terminate_connection(self, connection_id: str) -> Connection:
        connection = await self.conn_repo.get_connection(connection_id)
        if connection is None:
            raise ConnectionNotFoundError(connection_id)
        if connection.state == ConnectionState.CLOSED:
            raise ConnectionStateError(
                f"Connection '{connection_id}' is already closed."
            )
        if connection.state == ConnectionState.PENDING:
            return await self._terminate_pending(connection)
        terminate_resp = await self.conn_client.terminate_connection(connection_id)
        if terminate_resp.status != "succeeded":
            raise ConnectionStateError(
                f"Failed to terminate connection '{connection_id}': {terminate_resp.status}"
            )
        connection = await self.conn_repo.patch_connection(
            connection_id, PatchConnectionRequest(state=ConnectionState.CLOSED)
        )
        if not connection:
            raise ConnectionNotFoundError(connection_id)
        account = await self._billing_account_for_node(connection.node_id)
        if account is not None:
            await self.entitlements_bucket_service.release_connection_slot(account.id)
        logger.bind(connection_id=connection.id).info("Connection closed")
        await self.sync_connections()
        return connection

    async def _terminate_pending(self, connection: Connection) -> Connection:
        """Cancel a connection that never reached a worker."""
        if self.task_service is not None:
            await self.task_service.cancel_for_connection(
                connection.id, "terminated by user"
            )
        prior = await self.conn_repo.close_pending(connection.id)
        if prior is None:
            # Provisioned (or closed) between our read and the update; the
            # normal path handles whatever it is now.
            return await self.terminate_connection(connection.id)
        if prior == Status.IN_PROGRESS:  # a failed one already gave its slot back
            account = await self._billing_account_for_node(connection.node_id)
            if account is not None:
                await self.entitlements_bucket_service.release_connection_slot(
                    account.id
                )
        logger.bind(connection_id=connection.id).info("Pending connection cancelled")
        closed = await self.conn_repo.get_connection(connection.id)
        if closed is None:
            raise ConnectionNotFoundError(connection.id)
        return closed

    async def delete_connection(self, connection_id: str) -> None:
        connection = await self.conn_repo.get_connection(connection_id)
        if connection is None:
            raise ConnectionNotFoundError(connection_id)
        failed_pending = (
            connection.state == ConnectionState.PENDING
            and connection.status == Status.FAILED
        )
        if connection.state != ConnectionState.CLOSED and not failed_pending:
            raise ConnectionStateError(
                f"Connection '{connection_id}' is not closed. Cannot delete an active connection."
            )
        deleted = await self.conn_repo.delete_connection(connection_id)
        if not deleted:
            raise ConnectionNotFoundError(connection_id)
        logger.bind(connection_id=connection_id).info("Connection deleted")
        await self.sync_connections()

    async def wait_for_sync(self) -> None:
        await self._sync_connections()

    async def sync_connections(self) -> asyncio.Task[None]:
        logger.debug("starting sync in background")
        task = asyncio.create_task(self._sync_connections())
        task.add_done_callback(
            lambda t: (
                logger.debug("background sync completed")
                if not t.exception()
                else logger.error(f"background sync failed: {t.exception()}")
            )
        )
        return task

    @alru_cache(maxsize=1, ttl=5)
    async def _sync_connections(self) -> None:
        if not await self.conn_client.available():
            # No worker VM right now. Listing nothing would read as "every
            # connection is gone", so leave the local view untouched.
            logger.debug("Sync skipped: no conn_worker available")
            return
        # Taken *before* listing: anything created locally after this instant
        # can't be in the snapshot, so must not be mistaken for "gone".
        snapshot_at = datetime.now(UTC)
        worker_conns = {c.id: c for c in await self.conn_client.list_connections()}
        new = changed = 0

        for batch in batched(worker_conns, 100):
            existing_ids = set(await self.conn_repo.search_conn_ids(list(batch)))

            new_conns = [worker_conns[i] for i in batch if i not in existing_ids]
            if new_conns:
                await self.conn_repo.batch_create(new_conns)
                new += len(new_conns)

            changes: dict[str, PatchConnectionRequest] = {}
            for existing in await self.conn_repo.get_connections_by_ids(
                list(existing_ids)
            ):
                worker = worker_conns[existing.id]
                if (
                    worker.state,
                    worker.status,
                    worker.name,
                    worker.closed_at,
                ) != (
                    existing.state,
                    existing.status,
                    existing.name,
                    existing.closed_at,
                ):
                    changes[existing.id] = PatchConnectionRequest(
                        name=worker.name,
                        state=worker.state,
                        status=worker.status,
                        closed_at=worker.closed_at,
                    )
            if changes:
                await self.conn_repo.batch_patch_connections(changes)
                changed += len(changes)

        # conn_worker deletes a connection's record when it terminates or the
        # tunnel drops, so "gone from the worker" means closed. Without this a
        # dropped connection would stay "running" locally forever.
        gone = [
            i
            for i in await self.conn_repo.list_open_ids(created_before=snapshot_at)
            if i not in worker_conns
        ]
        await self.conn_repo.mark_closed(gone)

        logger.info(f"Sync complete: {new} new, {changed} changed, {len(gone)} closed")
