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


class ConnectionNotFoundError(Exception):
    def __init__(self, connection_id: str) -> None:
        super().__init__(f"Connection '{connection_id}' not found")
        self.connection_id = connection_id


class ConnectionService:
    def __init__(
        self, conn_repo: ConnectionRepository, connClient: ConnWorkerClient
    ) -> None:
        self.conn_repo = conn_repo
        self.connClient = connClient

    async def create_connection(self, req: CreateConnectionRequest) -> Connection:
        connection = await self.connClient.new_connection(req)
        connection = await self.conn_repo.create_connection(connection)
        logger.bind(connection_id=connection.id).info("Connection created")
        return connection

    async def get_connections(
        self, req: QueryConnectionRequest
    ) -> ListConnectionResponse:
        items = await self.conn_repo.query_connections(req)
        return ListConnectionResponse(items=items, total=len(items))

    async def get_connection(self, connection_id: str) -> Connection:
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
        return connection

    async def terminate_connection(self, connection_id: str) -> Connection:
        connection = await self.conn_repo.get_connection(connection_id)
        if connection is None:
            raise ConnectionNotFoundError(connection_id)
        if connection.state == ConnectionState.CLOSED:
            raise Exception(f"Connection '{connection_id}' is already closed.")
        terminate_req = TerminateConnRequest(connection_id=connection_id)
        terminate_resp = await self.connClient.terminate_connection(terminate_req)
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
