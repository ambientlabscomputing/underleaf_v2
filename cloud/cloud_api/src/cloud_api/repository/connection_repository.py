from datetime import UTC, datetime

from sqlalchemy import and_, delete, func, insert, not_, select, update

from cloud_api.models.api import (
    Connection,
    ConnectionState,
    PatchConnectionRequest,
    QueryConnectionRequest,
    Status,
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
                status_detail=conn.status_detail,
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
                # A pending connection that gave up already released its slot.
                .where(
                    not_(
                        and_(
                            SQLConnection.state == ConnectionState.PENDING.value,
                            SQLConnection.status == Status.FAILED.value,
                        )
                    )
                )
            )
            return int(result.scalar_one())

    async def list_open_ids(self, created_before: datetime) -> list[str]:
        """Open connections conn_worker should know about. Pending ones are
        excluded (they don't exist on a worker yet) and the cutoff is the last
        *update*, not creation: a connection activated after the worker
        snapshot was taken was created long before it, yet isn't in it."""
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLConnection.id)
                .where(SQLConnection.state != "closed")
                .where(SQLConnection.state != ConnectionState.PENDING.value)
                .where(SQLConnection.updated_at < created_before)
            )
            return [str(i) for i in result.all()]

    # -- Pending (queued for a worker) connections ---------------------------
    # Each takes a row lock so the runner giving up, the runner activating and
    # a user terminating can't both win; the caller learns which one did.

    async def _lock_pending(self, session, connection_id: str) -> SQLConnection | None:
        return await session.scalar(
            select(SQLConnection)
            .where(SQLConnection.id == connection_id)
            .where(SQLConnection.state == ConnectionState.PENDING.value)
            .with_for_update()
        )

    async def activate_pending(self, connection_id: str, created: Connection) -> bool:
        """The worker now has the connection: adopt its state/status.
        False if it was terminated or failed in the meantime."""
        async with self.get_session() as session:
            row = await self._lock_pending(session, connection_id)
            if row is None or row.status != Status.IN_PROGRESS.value:
                return False
            row.state = created.state.value
            row.status = created.status.value
            row.status_detail = None
            row.updated_at = datetime.now(UTC)
            await session.commit()
            return True

    async def fail_pending(self, connection_id: str, detail: str) -> bool:
        """Give up on a pending connection. True only for the caller that
        moved it, who is then responsible for releasing the slot."""
        async with self.get_session() as session:
            row = await self._lock_pending(session, connection_id)
            if row is None or row.status != Status.IN_PROGRESS.value:
                return False
            row.status = Status.FAILED.value
            row.status_detail = detail
            row.updated_at = datetime.now(UTC)
            await session.commit()
            return True

    async def close_pending(self, connection_id: str) -> Status | None:
        """Terminate a pending connection. Returns the status it had (so the
        caller knows whether a slot is still held: only IN_PROGRESS holds
        one), or None if it wasn't pending."""
        async with self.get_session() as session:
            row = await self._lock_pending(session, connection_id)
            if row is None:
                return None
            prior = Status(row.status)
            now = datetime.now(UTC)
            row.state = ConnectionState.CLOSED.value
            row.closed_at = now
            row.status_detail = None
            row.updated_at = now
            await session.commit()
            return prior

    async def set_pending_detail(self, connection_id: str, detail: str) -> bool:
        """Progress text; a no-op (no write) when unchanged so a waiting task
        polling every few seconds doesn't churn the row."""
        async with self.get_session() as session:
            result = await session.execute(
                update(SQLConnection)
                .where(SQLConnection.id == connection_id)
                .where(SQLConnection.state == ConnectionState.PENDING.value)
                .where(SQLConnection.status == Status.IN_PROGRESS.value)
                .where(SQLConnection.status_detail.is_distinct_from(detail))
                .values(status_detail=detail, updated_at=datetime.now(UTC))
            )
            await session.commit()
            return result.rowcount > 0

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
