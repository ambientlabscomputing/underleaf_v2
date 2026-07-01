import grpc.aio
from google.protobuf import empty_pb2

from cloud_api.lib.conn_worker_client.conn_worker_pb2 import (
    CloseStreamRequest,
    CloseStreamResponse,
    Connection,
    CreateConnectionRequest,
    GetConnectionRequest,
    GetStreamRequest,
    NewStreamRequest,
    Stream,
    TerminateConnectionRequest,
)
from cloud_api.lib.conn_worker_client.conn_worker_pb2_grpc import ConnectionWorkerStub


class ConnWorkerClient:
    def __init__(self, target: str):
        self._target = target
        self._channel: grpc.aio.Channel | None = None
        self._stub: ConnectionWorkerStub | None = None

    def _get_stub(self) -> ConnectionWorkerStub:
        """Lazily create the channel and stub on first use (must be inside an async context)."""
        if self._channel is None:
            self._channel = grpc.aio.insecure_channel(self._target)
            self._stub = ConnectionWorkerStub(self._channel)
        return self._stub  # type: ignore[return-value]

    async def new_connection(self, node_id: str, name: str) -> Connection:
        return await self._get_stub().CreateConnection(
            CreateConnectionRequest(node_id=node_id, name=name)
        )

    async def get_connection(self, connection_id: str) -> Connection:
        return await self._get_stub().GetConnection(GetConnectionRequest(id=connection_id))

    async def terminate_connection(self, connection_id: str) -> None:
        await self._get_stub().TerminateConnection(
            TerminateConnectionRequest(id=connection_id)
        )

    async def list_connections(self) -> list[Connection]:
        result = []
        async for conn in self._get_stub().ListConnections(empty_pb2.Empty()):
            result.append(conn)
        return result

    async def new_stream(
        self, connection_id: str, type: str, endpoint: str, port: int
    ) -> Stream:
        return await self._get_stub().NewStream(
            NewStreamRequest(
                connection_id=connection_id,
                type=type,
                endpoint=endpoint,
                port=port,
            )
        )

    async def close_stream(self, stream_id: str) -> CloseStreamResponse:
        return await self._get_stub().CloseStream(CloseStreamRequest(stream_id=stream_id))

    async def list_streams(self) -> list[Stream]:
        result = []
        async for stream in self._get_stub().ListStreams(empty_pb2.Empty()):
            result.append(stream)
        return result

    async def get_stream(self, stream_id: str) -> Stream:
        return await self._get_stub().GetStream(GetStreamRequest(id=stream_id))

    async def close(self) -> None:
        if self._channel is not None:
            await self._channel.close()
