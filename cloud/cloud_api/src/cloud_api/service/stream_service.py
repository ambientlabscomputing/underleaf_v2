import asyncio
from cloud_api.models.api import (
    CloseStreamRequest,
    Stream,
    ListStreamResponse,
    PatchStreamRequest,
    QueryStreamRequest,
    NewStreamRequest,
    StreamState,
)
from cloud_api.repository.stream_repository import StreamRepository
from cloud_api import logger
from cloud_api.lib.conn_worker_client.conn_worker_client import ConnWorkerClient
from async_lru import alru_cache
from itertools import batched


class StreamNotFoundError(Exception):
    def __init__(self, stream_id: str) -> None:
        super().__init__(f"Stream '{stream_id}' not found")
        self.stream_id = stream_id


class StreamService:
    def __init__(
        self, stream_repo: StreamRepository, conn_worker_client: ConnWorkerClient
    ) -> None:
        self.stream_repo = stream_repo
        self.conn_worker_client = conn_worker_client

    async def create_stream(self, req: NewStreamRequest) -> Stream:
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
            raise Exception(f"Stream '{stream_id}' is already closed.")
        _ = await self.conn_worker_client.close_stream(stream_id)
        updated_stream = await self.stream_repo.patch_stream(
            stream_id, PatchStreamRequest(state=StreamState.CLOSED)
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
            raise Exception(
                f"Stream '{stream_id}' is not closed. Cannot delete an active stream."
            )
        deleted = await self.stream_repo.delete_stream(stream_id)
        if not deleted:
            raise StreamNotFoundError(stream_id)
        logger.bind(stream_id=stream_id).info("Stream deleted")

    async def wait_for_sync(self) -> None:
        await self._sync_streams()

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
        worker_streams = await self.conn_worker_client.list_streams()
        new = 0
        changed = 0
        deleted = 0
        for i, worker_stream_batch in enumerate(batched(worker_streams, 100)):
            worker_streams_mapped = {
                stream.id: stream for stream in worker_stream_batch
            }
            unique_worker_stream_ids = set(worker_streams_mapped.keys())
            existing_stream_ids = await self.stream_repo.search_stream_ids(
                list(unique_worker_stream_ids)
            )

            new_stream_ids = unique_worker_stream_ids - set(existing_stream_ids)
            new_streams = [
                worker_streams_mapped[stream_id] for stream_id in new_stream_ids
            ]
            if new_streams:
                await self.stream_repo.batch_create(new_streams)
                new += len(new_streams)

            existing_streams = await self.stream_repo.get_streams_by_ids(
                list(existing_stream_ids)
            )
            changes: dict[str, PatchStreamRequest] = {}
            for existing_stream in existing_streams:
                worker_stream = worker_streams_mapped.get(existing_stream.id)
                if worker_stream != existing_stream:
                    if not worker_stream:
                        continue
                    changes[existing_stream.id] = PatchStreamRequest(
                        name=worker_stream.name,
                        state=worker_stream.state,
                        status=worker_stream.status,
                        closed_at=worker_stream.closed_at,
                    )

            if changes:
                await self.stream_repo.batch_patch_streams(changes)
                changed += len(changes)
            logger.debug(
                f"Sync batch {i + 1}: {len(new_streams)} new, {len(changes)} changed"
            )
        logger.info(
            f"Stream sync complete: {new} new, {changed} changed, {deleted} deleted"
        )
