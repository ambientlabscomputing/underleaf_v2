import asyncio
from datetime import UTC, datetime
from itertools import batched

from async_lru import alru_cache

from cloud_api import logger
from cloud_api.lib.conn_worker_client.conn_worker_client import ConnWorkerClient
from cloud_api.models.api import (
    BillingAccount,
    ListStreamResponse,
    NewStreamRequest,
    PatchStreamRequest,
    QueryStreamRequest,
    Stream,
    StreamState,
    UsageEventType,
    UsageUnits,
)
from cloud_api.repository.billing_account_repository import BillingAccountRepository
from cloud_api.repository.connection_repository import ConnectionRepository
from cloud_api.repository.stream_repository import StreamRepository
from cloud_api.service.entitlements_bucket_service import EntitlementsBucketService
from cloud_api.service.usage_enforcement_service import UsageEnforcementService
from cloud_api.service.usage_event_service import UsageEventService


class StreamNotFoundError(Exception):
    def __init__(self, stream_id: str) -> None:
        super().__init__(f"Stream '{stream_id}' not found")
        self.stream_id = stream_id


class StreamStateError(Exception):
    """The operation isn't valid for the stream's current state."""


class InsufficientNetworkTrafficBalanceError(Exception):
    def __init__(self, billing_account_id: str) -> None:
        super().__init__(
            f"BillingAccount '{billing_account_id}' has no network traffic balance"
        )
        self.billing_account_id = billing_account_id


class StreamService:
    def __init__(
        self,
        stream_repo: StreamRepository,
        conn_worker_client: ConnWorkerClient,
        conn_repo: ConnectionRepository,
        billing_account_repo: BillingAccountRepository,
        entitlements_bucket_service: EntitlementsBucketService,
        usage_event_service: UsageEventService,
        usage_enforcement_service: UsageEnforcementService,
    ) -> None:
        self.stream_repo = stream_repo
        self.conn_worker_client = conn_worker_client
        self.conn_repo = conn_repo
        self.billing_account_repo = billing_account_repo
        self.entitlements_bucket_service = entitlements_bucket_service
        self.usage_event_service = usage_event_service
        self.usage_enforcement_service = usage_enforcement_service

    async def get_owner_principal_id(self, stream_id: str) -> str | None:
        return await self.stream_repo.get_principal_account_id(stream_id)

    async def get_connection_owner_principal_id(self, connection_id: str) -> str | None:
        return await self.conn_repo.get_principal_account_id(connection_id)

    async def _billing_account_for_connection(
        self, connection_id: str
    ) -> BillingAccount | None:
        connection = await self.conn_repo.get_connection(connection_id)
        if connection is None:
            return None
        return await self.billing_account_repo.get_billing_account_by_node_id(
            connection.node_id
        )

    async def create_stream(self, req: NewStreamRequest) -> Stream:
        account = await self._billing_account_for_connection(req.connection_id)
        # Traffic can't be pre-charged, so this gates *new* streams only;
        # already-open ones are stopped by the usage sync when the allowance
        # (and any grace) is spent.
        if (
            account is not None
            and not await self.entitlements_bucket_service.has_network_traffic_balance(
                account.id
            )
        ):
            raise InsufficientNetworkTrafficBalanceError(account.id)
        stream = await self.conn_worker_client.new_stream(
            connection_id=req.connection_id,
            type=req.type_,
            endpoint=req.endpoint or "",
            port=req.port or 0,
        )
        stream = await self.stream_repo.create_stream(stream)
        logger.bind(stream_id=stream.id).info("Stream created")
        return stream

    async def get_streams(self, req: QueryStreamRequest) -> ListStreamResponse:
        items = await self.stream_repo.query_streams(req)
        return ListStreamResponse(items=items, total=len(items))

    async def get_stream(self, stream_id: str) -> Stream:
        stream = await self.stream_repo.get_stream(stream_id)
        if stream is None:
            raise StreamNotFoundError(stream_id)
        return stream

    async def patch_stream(self, stream_id: str, req: PatchStreamRequest) -> Stream:
        stream = await self.stream_repo.patch_stream(stream_id, req)
        if stream is None:
            raise StreamNotFoundError(stream_id)
        return stream

    async def close_stream(self, stream_id: str) -> Stream:
        stream = await self.stream_repo.get_stream(stream_id)
        if stream is None:
            raise StreamNotFoundError(stream_id)
        if stream.state == StreamState.CLOSED:
            raise StreamStateError(f"Stream '{stream_id}' is already closed.")
        await self.conn_worker_client.close_stream(stream_id)
        updated_stream = await self.stream_repo.patch_stream(
            stream_id,
            PatchStreamRequest(state=StreamState.CLOSED, closed_at=datetime.now(UTC)),
        )
        if not updated_stream:
            raise StreamNotFoundError(stream_id)
        logger.bind(stream_id=stream_id).info("Stream closed")
        return updated_stream

    async def delete_stream(self, stream_id: str) -> None:
        stream = await self.stream_repo.get_stream(stream_id)
        if stream is None:
            raise StreamNotFoundError(stream_id)
        if stream.state != StreamState.CLOSED:
            raise StreamStateError(
                f"Stream '{stream_id}' is not closed. Cannot delete an active stream."
            )
        deleted = await self.stream_repo.delete_stream(stream_id)
        if not deleted:
            raise StreamNotFoundError(stream_id)
        logger.bind(stream_id=stream_id).info("Stream deleted")

    # -- Usage metering & enforcement ----------------------------------------

    async def sync_usage_from_conn_worker(self) -> None:
        """Turn conn_worker's cumulative per-stream byte counters into usage
        events and allowance charges, and stop relayed traffic for accounts
        that have run out.

        Charging is at-most-once: the usage event (unique on its idempotency
        key) is written first and the charge only happens for the call that
        created it, so a crash or retry between steps can under-charge by one
        delta but never double-charge a customer.
        """
        stopped: set[str] = set()
        for worker_stream in await self.conn_worker_client.list_streams():
            total_bytes = worker_stream.bytes_in + worker_stream.bytes_out
            if total_bytes <= 0:
                continue
            local = await self.stream_repo.get_stream(worker_stream.id)
            if local is None:
                continue  # adopted by the stream sync first; billed next tick
            delta = total_bytes - local.last_reported_bytes
            if delta <= 0:
                continue
            account = await self._billing_account_for_connection(local.connection_id)
            if account is None:
                logger.bind(stream_id=local.id).warning(
                    "Could not resolve billing account for stream; usage not recorded"
                )
                continue

            now = datetime.now(UTC)
            _, created = await self.usage_event_service.record_usage(
                billing_account_id=account.id,
                event_type=UsageEventType.DATA_USAGE,
                usage_unit=UsageUnits.BYTES,
                usage_amount=delta,
                period_start=now,
                period_end=now,
                idempotency_key=f"{local.id}:{local.last_reported_bytes}:{total_bytes}",
                stream_id=local.id,
            )
            hard_stop = False
            if created:
                hard_stop = await self.usage_enforcement_service.record_traffic(
                    account.id, delta
                )
            await self.stream_repo.update_last_reported_bytes(local.id, total_bytes)
            if hard_stop and account.id not in stopped:
                stopped.add(account.id)
                await self._stop_relayed_traffic(account)

    async def _stop_relayed_traffic(self, account: BillingAccount) -> None:
        """Hard stop: close every active stream for the account so the
        gateway stops relaying. New streams are refused by create_stream
        until a renewal or top-up replenishes the allowance."""
        active = await self.stream_repo.query_streams(
            QueryStreamRequest(
                principal_account_id=account.principal_account_id,
                state=StreamState.ACTIVE,
            )
        )
        logger.bind(billing_account_id=account.id).warning(
            f"Allowance exhausted: stopping {len(active)} active stream(s)"
        )
        for stream in active:
            try:
                await self.close_stream(stream.id)
            except Exception as exc:  # noqa: BLE001 -- keep stopping the rest
                logger.bind(stream_id=stream.id).error(
                    f"Failed to stop stream during hard stop: {exc}"
                )

    # -- Worker <-> local state sync -----------------------------------------

    async def wait_for_sync(self) -> None:
        await self._sync_streams()
        await self.sync_usage_from_conn_worker()

    async def sync_streams(self) -> asyncio.Task[None]:
        logger.debug("starting stream sync in background")
        task = asyncio.create_task(self._sync_streams())
        task.add_done_callback(
            lambda t: (
                logger.debug("background stream sync completed")
                if not t.exception()
                else logger.error(f"background stream sync failed: {t.exception()}")
            )
        )
        return task

    @alru_cache(maxsize=1, ttl=5)
    async def _sync_streams(self) -> None:
        snapshot_at = datetime.now(UTC)
        worker_streams = {s.id: s for s in await self.conn_worker_client.list_streams()}
        new = changed = 0

        for batch in batched(worker_streams, 100):
            existing_ids = set(await self.stream_repo.search_stream_ids(list(batch)))

            # A stream whose connection isn't known locally yet can't be
            # inserted (FK); it's adopted on a later tick once the connection
            # sync has caught up.
            candidates = [worker_streams[i] for i in batch if i not in existing_ids]
            known_conns = set(
                await self.conn_repo.search_conn_ids(
                    list({s.connection_id for s in candidates})
                )
            )
            new_streams = [s for s in candidates if s.connection_id in known_conns]
            if new_streams:
                await self.stream_repo.batch_create(new_streams)
                new += len(new_streams)

            changes: dict[str, PatchStreamRequest] = {}
            for existing in await self.stream_repo.get_streams_by_ids(
                list(existing_ids)
            ):
                worker = worker_streams[existing.id]
                if (worker.state, worker.status, worker.closed_at) != (
                    existing.state,
                    existing.status,
                    existing.closed_at,
                ):
                    changes[existing.id] = PatchStreamRequest(
                        state=worker.state,
                        status=worker.status,
                        closed_at=worker.closed_at,
                    )
            if changes:
                await self.stream_repo.batch_patch_streams(changes)
                changed += len(changes)

        # Records that expired or were removed on the worker can no longer
        # report state, so their local rows are closed.
        gone = [
            i
            for i in await self.stream_repo.list_active_ids(created_before=snapshot_at)
            if i not in worker_streams
        ]
        await self.stream_repo.mark_closed(gone)

        logger.info(
            f"Stream sync complete: {new} new, {changed} changed, {len(gone)} closed"
        )
