from datetime import UTC, datetime

from sqlalchemy import delete, insert, select, update

from cloud_api.models.api import (
    PatchStreamRequest,
    QueryStreamRequest,
    Stream,
)
from cloud_api.models.sql import (
    SQLCluster,
    SQLConnection,
    SQLNode,
    SQLStream,
)
from cloud_api.repository.base_repository import BaseRepository


class StreamRepository(BaseRepository):
    async def create_stream(self, stream: Stream) -> Stream:
        async with self.get_session() as session:
            new_stream = SQLStream(
                # Must reuse conn_worker's id: it is the only key the sync
                # loop and usage metering can match local rows against.
                id=stream.id,
                name=stream.name,
                connection_id=stream.connection_id,
                created_at=datetime.now(UTC),
                updated_at=datetime.now(UTC),
                type=stream.type_,
                state=stream.state,
                status=stream.status,
                endpoint=stream.endpoint,
                port=stream.port,
            )
            session.add(new_stream)
            await session.commit()
            await session.refresh(new_stream)
            return Stream.model_validate(new_stream)

    async def get_stream(self, stream_id: str) -> Stream | None:
        async with self.get_session() as session:
            stream = await session.get(SQLStream, stream_id)
            if stream is None:
                return None
            return Stream.model_validate(stream)

    async def get_principal_account_id(self, stream_id: str) -> str | None:
        """The tenant that owns a stream (stream -> connection -> node -> cluster)."""
        async with self.get_session() as session:
            return await session.scalar(
                select(SQLCluster.principal_account_id)
                .select_from(SQLStream)
                .join(SQLConnection, SQLStream.connection_id == SQLConnection.id)
                .join(SQLNode, SQLConnection.node_id == SQLNode.id)
                .join(SQLCluster, SQLNode.cluster_id == SQLCluster.id)
                .where(SQLStream.id == stream_id)
            )

    async def query_streams(self, req: QueryStreamRequest) -> list[Stream]:
        async with self.get_session() as session:
            query = (
                select(SQLStream)
                .join(SQLConnection, SQLStream.connection_id == SQLConnection.id)
                .join(SQLNode, SQLConnection.node_id == SQLNode.id)
                .join(SQLCluster, SQLNode.cluster_id == SQLCluster.id)
            )
            if req.principal_account_id is not None:
                query = query.filter(
                    SQLCluster.principal_account_id == req.principal_account_id
                )
            if req.node_id is not None:
                query = query.filter(SQLNode.id == req.node_id)
            # Select.filter() takes expressions, not kwargs, so build them
            # explicitly against SQLStream (filter_by would bind to the last
            # joined entity, SQLCluster).
            filters = req.model_dump(
                exclude_none=True, exclude={"principal_account_id", "node_id"}
            )
            for field, value in filters.items():
                query = query.where(getattr(SQLStream, field) == value)
            result = await session.scalars(query)
            streams = result.all()
            return [Stream.model_validate(stream) for stream in streams]

    async def patch_stream(
        self, stream_id: str, req: PatchStreamRequest
    ) -> Stream | None:
        async with self.get_session() as session:
            update_stmt = (
                update(SQLStream)
                .where(SQLStream.id == stream_id)
                .values(updated_at=datetime.now(UTC))
            )
            for field, value in req.model_dump(exclude_none=True).items():
                update_stmt = update_stmt.values(**{field: value})
            result = await session.execute(update_stmt)
            if result.rowcount == 0:
                return None
            await session.commit()
            updated_stream = await session.get(SQLStream, stream_id)
            return Stream.model_validate(updated_stream)

    async def delete_stream(self, stream_id: str) -> bool:
        async with self.get_session() as session:
            stream = await session.get(SQLStream, stream_id)
            if stream is None:
                return False
            await session.delete(stream)
            await session.commit()
            return True

    async def search_stream_ids(self, search_ids: list[str]) -> list[str]:
        async with self.get_session() as session:
            query = select(SQLStream.id).where(SQLStream.id.in_(search_ids))
            result = await session.scalars(query)
            return [str(id) for id in result.all()]

    async def batch_create(self, streams: list[Stream]) -> None:
        batch = [stream.model_dump(by_alias=True) for stream in streams]
        async with self.get_session() as session:
            await session.execute(insert(SQLStream), batch)
            await session.commit()

    async def get_streams_by_ids(self, stream_ids: list[str]) -> list[Stream]:
        async with self.get_session() as session:
            query = select(SQLStream).where(SQLStream.id.in_(stream_ids))
            result = await session.scalars(query)
            streams = result.all()
            return [Stream.model_validate(stream) for stream in streams]

    async def batch_patch_streams(self, changes: dict[str, PatchStreamRequest]) -> None:
        async with self.get_session() as session:
            batch = [
                {
                    "id": stream_id,
                    **change.model_dump(exclude_unset=True, exclude_none=True),
                    "updated_at": datetime.now(UTC),
                }
                for stream_id, change in changes.items()
            ]
            await session.execute(update(SQLStream), batch)
            await session.commit()

    async def list_active_ids(self, created_before: datetime) -> list[str]:
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLStream.id)
                .where(SQLStream.state == "active")
                .where(SQLStream.created_at < created_before)
            )
            return [str(i) for i in result.all()]

    async def mark_closed(self, stream_ids: list[str]) -> None:
        """Close streams conn_worker no longer knows about (record expired or
        removed): their state can't be read back, so closed is the only
        truthful answer."""
        if not stream_ids:
            return
        now = datetime.now(UTC)
        async with self.get_session() as session:
            await session.execute(
                update(SQLStream)
                .where(SQLStream.id.in_(stream_ids))
                .values(state="closed", closed_at=now, updated_at=now)
            )
            await session.commit()

    async def update_last_reported_bytes(
        self, stream_id: str, last_reported_bytes: int
    ) -> Stream | None:
        """Internal bookkeeping update, deliberately separate from
        patch_stream/PatchStreamRequest so it can't be set by a client."""
        async with self.get_session() as session:
            update_stmt = (
                update(SQLStream)
                .where(SQLStream.id == stream_id)
                .values(
                    last_reported_bytes=last_reported_bytes,
                    updated_at=datetime.now(UTC),
                )
            )
            result = await session.execute(update_stmt)
            if result.rowcount == 0:
                return None
            await session.commit()
            updated_stream = await session.get(SQLStream, stream_id)
            return Stream.model_validate(updated_stream)

    async def batch_delete_streams(self, stream_ids: list[str]) -> None:
        async with self.get_session() as session:
            await session.execute(delete(SQLStream).where(SQLStream.id.in_(stream_ids)))
            await session.commit()
