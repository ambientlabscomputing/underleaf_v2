import asyncio
from cloud_api.models.api import (
    Connection,
    ConnectionState,
    CreateConnectionRequest,
    ListConnectionResponse,
    PatchConnectionRequest,
    QueryConnectionRequest,
    TerminateConnRequest,
)
from cloud_api.repository.connection_repository import ConnectionRepository
from cloud_api.lib.conn_worker_client import ConnWorkerClient
from cloud_api import logger
from async_lru import alru_cache
from itertools import batched


class ConnectionNotFoundError(Exception):
    def __init__(self, connection_id: str) -> None:
        super().__init__(f"Connection '{connection_id}' not found")
        self.connection_id = connection_id


class ConnectionService:
    def __init__(
        self, conn_repo: ConnectionRepository, conn_client: ConnWorkerClient
    ) -> None:
        self.conn_repo = conn_repo
        self.conn_client = conn_client

    async def create_connection(self, req: CreateConnectionRequest) -> Connection:
        connection = await self.conn_client.new_connection(req)
        connection = await self.conn_repo.create_connection(connection)
        logger.bind(connection_id=connection.id).info("Connection created")
        return connection

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
            raise Exception(f"Connection '{connection_id}' is already closed.")
        terminate_req = TerminateConnRequest(connection_id=connection_id)
        terminate_resp = await self.conn_client.terminate_connection(terminate_req)
        if terminate_resp.status != "succeeded":
            raise Exception(
                f"Failed to terminate connection '{connection_id}': {terminate_resp.status}"
            )
        connection = await self.conn_repo.patch_connection(
            connection_id, PatchConnectionRequest(state=ConnectionState.CLOSED)
        )
        if not connection:
            raise ConnectionNotFoundError(connection_id)
        logger.bind(connection_id=connection.id).info("Connection closed")
        await self.sync_connections()
        return connection

    async def delete_connection(self, connection_id: str) -> None:
        connection = await self.conn_repo.get_connection(connection_id)
        if connection is None:
            raise ConnectionNotFoundError(connection_id)
        if connection.state != ConnectionState.CLOSED:
            raise Exception(
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
        conn_worker_conns = await self.conn_client.list_connections()
        new = 0
        changed = 0
        deleted = 0
        for i, worker_conn_batch in enumerate(batched(conn_worker_conns, 100)):
            conn_worker_conns_mapped = {conn.id: conn for conn in worker_conn_batch}
            unique_worker_conn_ids = set(conn_worker_conns_mapped.keys())
            existing_conn_ids = await self.conn_repo.search_conn_ids(
                list(unique_worker_conn_ids)
            )

            # create connections that exist in the worker but not in the database
            new_conn_ids = unique_worker_conn_ids - set(existing_conn_ids)
            new_conns = [conn_worker_conns_mapped[conn_id] for conn_id in new_conn_ids]
            if new_conns:
                await self.conn_repo.batch_create(new_conns)
                new += len(new_conns)

            # grab existing connections in bulk and update if different
            existing_conns = await self.conn_repo.get_connections_by_ids(
                list(existing_conn_ids)
            )
            changes: dict[str, PatchConnectionRequest] = {}
            for existing_conn in existing_conns:
                worker_conn = conn_worker_conns_mapped.get(existing_conn.id)
                if worker_conn != existing_conn:
                    if not worker_conn:
                        continue
                    changes[existing_conn.id] = PatchConnectionRequest(
                        name=worker_conn.name,
                        state=worker_conn.state,
                        status=worker_conn.status,
                        closed_at=worker_conn.closed_at,
                    )

            if changes:
                await self.conn_repo.batch_patch_connections(changes)
                changed += len(changes)
            logger.debug(
                f"Sync batch {i + 1}: {len(new_conns)} new, {len(changes)} changed"
            )
        logger.info(f"Sync complete: {new} new, {changed} changed, {deleted} deleted")
