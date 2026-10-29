from datetime import UTC, datetime

from sqlalchemy import delete, func, insert, select, update

from cloud_api.models.api import (
    Connection,
    PatchConnectionRequest,
    QueryConnectionRequest,
)
from cloud_api.models.sql import SQLCluster, SQLConnection, SQLNode
from cloud_api.repository.base_repository import BaseRepository


class ConnectionRepository(BaseRepository):
    async def create_connection(self, conn: Connection) -> Connection:
        async with self.get_session() as session:
            new_conn = SQLConnection(
                id=conn.id,
                name=conn.name,
                node_id=conn.node_id,
                created_at=datetime.now(UTC),
                updated_at=datetime.now(UTC),
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

    async def get_principal_account_id(self, connection_id: str) -> str | None:
        """The tenant that owns a connection (connection -> node -> cluster)."""
        async with self.get_session() as session:
            return await session.scalar(
                select(SQLCluster.principal_account_id)
                .select_from(SQLConnection)
                .join(SQLNode, SQLConnection.node_id == SQLNode.id)
                .join(SQLCluster, SQLNode.cluster_id == SQLCluster.id)
                .where(SQLConnection.id == connection_id)
            )

    async def get_node_principal_account_id(self, node_id: str) -> str | None:
        """The tenant that owns a node (node -> cluster)."""
        async with self.get_session() as session:
            return await session.scalar(
                select(SQLCluster.principal_account_id)
                .select_from(SQLNode)
                .join(SQLCluster, SQLNode.cluster_id == SQLCluster.id)
                .where(SQLNode.id == node_id)
            )

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
            filters = req.model_dump(
                exclude_none=True, exclude={"principal_account_id"}
            )
            for field, value in filters.items():
                query = query.where(getattr(SQLConnection, field) == value)
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
                .values(updated_at=datetime.now(UTC))
            )
            for field, value in req.model_dump(exclude_none=True).items():
                update_stmt = update_stmt.values(**{field: value})
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

    async def count_open_for_principal(self, principal_account_id: str) -> int:
        """Connections not yet closed across all of a tenant's clusters --
        the ground truth connection slots are reconciled against."""
        async with self.get_session() as session:
            result = await session.execute(
                select(func.count(SQLConnection.id))
                .join(SQLNode, SQLConnection.node_id == SQLNode.id)
                .join(SQLCluster, SQLNode.cluster_id == SQLCluster.id)
                .where(SQLCluster.principal_account_id == principal_account_id)
                .where(SQLConnection.state != "closed")
            )
            return int(result.scalar_one())

    async def list_open_ids(self, created_before: datetime) -> list[str]:
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLConnection.id)
                .where(SQLConnection.state != "closed")
                .where(SQLConnection.created_at < created_before)
            )
            return [str(i) for i in result.all()]

    async def mark_closed(self, connection_ids: list[str]) -> None:
        """Close connections conn_worker no longer knows about (terminated
        or dropped): conn_worker deletes their record, so they simply stop
        appearing in ListConnections."""
        if not connection_ids:
            return
        now = datetime.now(UTC)
        async with self.get_session() as session:
            await session.execute(
                update(SQLConnection)
                .where(SQLConnection.id.in_(connection_ids))
                .values(state="closed", closed_at=now, updated_at=now)
            )
            await session.commit()

    async def search_conn_ids(self, search_ids: list[str]) -> list[str]:
        async with self.get_session() as session:
            query = select(SQLConnection.id).where(SQLConnection.id.in_(search_ids))
            result = await session.scalars(query)
            return [str(id) for id in result.all()]

    async def batch_create(self, connections: list[Connection]) -> None:
        batch = [conn.model_dump() for conn in connections]
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
                    **change.model_dump(exclude_unset=True, exclude_none=True),
                    "updated_at": datetime.now(UTC),
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
