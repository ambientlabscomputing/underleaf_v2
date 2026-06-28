from cloud_api.models.api import (
    Stream,
    QueryStreamRequest,
    PatchStreamRequest,
)
from cloud_api.models.sql import SQLConnection, SQLConnection, SQLStream, SQLCluster, SQLNode
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update
from datetime import datetime, timezone


class StreamRepository(BaseRepository):
    async def create_stream(self, stream: Stream) -> Stream:
        async with self.get_session() as session:
            new_stream = SQLStream(
                name=stream.name,
                connection_id=stream.connection_id,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
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
            query = query.filter(
                **req.model_dump(
                    exclude_unset=True,
                    exclude_none=True, 
                    exclude={"principal_account_id", "node_id"}
                )
            )
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
                .values(updated_at=datetime.now(timezone.utc))
            )
            if req.name is not None:
                update_stmt = update_stmt.values(name=req.name)
            result = await session.execute(update_stmt)
            if hasattr(result, "rowcount") and result.rowcount == 0:
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
