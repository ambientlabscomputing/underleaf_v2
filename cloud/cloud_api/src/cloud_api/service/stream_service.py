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
from cloud_api.lib.conn_worker_client import ConnWorkerClient


class StreamNotFoundError(Exception):
    def __init__(self, stream_id: str) -> None:
        super().__init__(f"Stream '{stream_id}' not found")
        self.stream_id = stream_id


class StreamService:
    def __init__(self, stream_repo: StreamRepository, conn_worker_client: ConnWorkerClient) -> None:
        self.stream_repo = stream_repo
        self.conn_worker_client = conn_worker_client

    async def create_stream(self, req: NewStreamRequest) -> Stream:
        stream = await self.conn_worker_client.new_stream(req)
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
        close_req = CloseStreamRequest(stream_id=stream_id)
        _ = await self.conn_worker_client.close_stream(close_req)
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
