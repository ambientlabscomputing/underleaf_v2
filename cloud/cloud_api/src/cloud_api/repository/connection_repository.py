from cloud_api.models.api import (
    Connection,
    CreateConnectionRequest,
    QueryConnectionRequest,
    PatchConnectionRequest,
)
from cloud_api.models.sql import SQLConnection, SQLCluster, SQLNode
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update, insert, delete
from datetime import datetime, timezone


class ConnectionRepository(BaseRepository):
    async def create_connection(self, conn: Connection) -> Connection:
        async with self.get_session() as session:
            new_conn = SQLConnection(
                id=conn.id,
                name=conn.name,
                node_id=conn.node_id,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
                closed_at=conn.closed_at,
                state=conn.state,
                status=conn.status,
            )
            session.add(new_conn)
            await session.commit()
            await session.refresh(new_conn)
            return Connection.model_validate(new_conn)

    async def get_connection(self, connection_id: str) -> Connection | None:
        async with self.get_session() as session:
            connection = await session.get(SQLConnection, connection_id)
            if connection is None:
                return None
            return Connection.model_validate(connection)

    async def query_connections(self, req: QueryConnectionRequest) -> list[Connection]:
        async with self.get_session() as session:
            query = (
                select(SQLConnection)
                .join(SQLNode, SQLConnection.node_id == SQLNode.id)
                .join(SQLCluster, SQLNode.cluster_id == SQLCluster.id)
            )
            if req.principal_account_id is not None:
                query = query.filter(
                    SQLCluster.principal_account_id == req.principal_account_id
                )
            query = query.filter(
                **req.model_dump(
                    exclude_unset=True,
                    exclude_none=True,
                    exclude={"principal_account_id"},
                )
            )
            result = await session.scalars(query)
            connections = result.all()
            return [Connection.model_validate(connection) for connection in connections]

    async def patch_connection(
        self, connection_id: str, req: PatchConnectionRequest
    ) -> Connection | None:
        async with self.get_session() as session:
            update_stmt = (
                update(SQLConnection)
                .where(SQLConnection.id == connection_id)
                .values(updated_at=datetime.now(timezone.utc))
            )
            if req.name is not None:
                update_stmt = update_stmt.values(name=req.name)
            result = await session.execute(update_stmt)
            if result.rowcount == 0:
                return None
            await session.commit()
            updated_connection = await session.get(SQLConnection, connection_id)
            return Connection.model_validate(updated_connection)

    async def delete_connection(self, connection_id: str) -> bool:
        async with self.get_session() as session:
            connection = await session.get(SQLConnection, connection_id)
            if connection is None:
                return False
            await session.delete(connection)
            await session.commit()
            return True

    async def search_conn_ids(self, search_ids: list[str]) -> list[str]:
        async with self.get_session() as session:
            query = select(SQLConnection.id).where(SQLConnection.id.in_(search_ids))
            result = await session.scalars(query)
            return [str(id) for id in result.all()]

    async def batch_create(self, connections: list[Connection]) -> None:
        batch = [
            conn.model_dump(
                mode="json",
            )
            for conn in connections
        ]
        async with self.get_session() as session:
            await session.execute(insert(SQLConnection), batch)
            await session.commit()

    async def get_connections_by_ids(
        self, connection_ids: list[str]
    ) -> list[Connection]:
        async with self.get_session() as session:
            query = select(SQLConnection).where(SQLConnection.id.in_(connection_ids))
            result = await session.scalars(query)
            connections = result.all()
            return [Connection.model_validate(connection) for connection in connections]

    async def batch_patch_connections(
        self, changes: dict[str, PatchConnectionRequest]
    ) -> None:
        async with self.get_session() as session:
            batch = [
                {
                    "id": conn_id,
                    **change.model_dump(
                        mode="json",
                        exclude_unset=True,
                        exclude_none=True,
                    ),
                    "updated_at": datetime.now(timezone.utc),
                }
                for conn_id, change in changes.items()
            ]
            await session.execute(update(SQLConnection), batch)
            await session.commit()

    async def batch_delete_connections(self, connection_ids: list[str]) -> None:
        async with self.get_session() as session:
            await session.execute(
                delete(SQLConnection).where(SQLConnection.id.in_(connection_ids))
            )
