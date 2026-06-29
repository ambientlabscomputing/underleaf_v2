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
        self._channel = grpc.aio.insecure_channel(target)
        self._stub = ConnectionWorkerStub(self._channel)

    async def new_connection(self, node_id: str, name: str) -> Connection:
        return await self._stub.CreateConnection(
            CreateConnectionRequest(node_id=node_id, name=name)
        )

    async def get_connection(self, connection_id: str) -> Connection:
        return await self._stub.GetConnection(
            GetConnectionRequest(id=connection_id)
        )

    async def terminate_connection(self, connection_id: str) -> None:
        await self._stub.TerminateConnection(
            TerminateConnectionRequest(id=connection_id)
        )

    async def list_connections(self) -> list[Connection]:
        result = []
        async for conn in self._stub.ListConnections(empty_pb2.Empty()):
            result.append(conn)
        return result

    async def new_stream(
        self, connection_id: str, type: str, endpoint: str, port: int
    ) -> Stream:
        return await self._stub.NewStream(
            NewStreamRequest(
                connection_id=connection_id,
                type=type,
                endpoint=endpoint,
                port=port,
            )
        )

    async def close_stream(self, stream_id: str) -> CloseStreamResponse:
        return await self._stub.CloseStream(
            CloseStreamRequest(stream_id=stream_id)
        )

    async def list_streams(self) -> list[Stream]:
        result = []
        async for stream in self._stub.ListStreams(empty_pb2.Empty()):
            result.append(stream)
        return result

    async def get_stream(self, stream_id: str) -> Stream:
        return await self._stub.GetStream(
            GetStreamRequest(id=stream_id)
        )

    async def close(self) -> None:
        await self._channel.close()
