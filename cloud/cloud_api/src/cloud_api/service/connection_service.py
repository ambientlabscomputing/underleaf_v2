from cloud_api.models.api import (
    Connection,
    CreateConnectionRequest,
    ListConnectionResponse,
    PatchConnectionRequest,
    QueryConnectionRequest,
)
from cloud_api.repository.connection_repository import ConnectionRepository
from cloud_api import logger


class ConnectionNotFoundError(Exception):
    def __init__(self, connection_id: str) -> None:
        super().__init__(f"Connection '{connection_id}' not found")
        self.connection_id = connection_id


class ConnectionService:
    def __init__(self, connection_repo: ConnectionRepository) -> None:
        self.connection_repo = connection_repo

    async def create_connection(self, req: CreateConnectionRequest) -> Connection:
        connection = await self.connection_repo.create_connection(req)
        logger.bind(connection_id=connection.id).info("Connection created")
        return connection

    async def get_connections(self, req: QueryConnectionRequest) -> ListConnectionResponse:
        items = await self.connection_repo.query_connections(req)
        return ListConnectionResponse(items=items, total=len(items))

    async def get_connection(self, connection_id: str) -> Connection:
        connection = await self.connection_repo.get_connection(connection_id)
        if connection is None:
            raise ConnectionNotFoundError(connection_id)
        return connection

    async def patch_connection(
        self, connection_id: str, req: PatchConnectionRequest
    ) -> Connection:
        connection = await self.connection_repo.patch_connection(connection_id, req)
        if connection is None:
            raise ConnectionNotFoundError(connection_id)
        return connection

    async def delete_connection(self, connection_id: str) -> None:
        deleted = await self.connection_repo.delete_connection(connection_id)
        if not deleted:
            raise ConnectionNotFoundError(connection_id)
        logger.bind(connection_id=connection_id).info("Connection deleted")
