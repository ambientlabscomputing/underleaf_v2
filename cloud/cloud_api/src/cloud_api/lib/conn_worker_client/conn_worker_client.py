import asyncio

import grpc
import grpc.aio
from google.protobuf import empty_pb2

from cloud_api.lib.conn_worker_client.conn_worker_pb2 import (
    CloseStreamRequest,
    CreateConnectionRequest,
    GetConnectionRequest,
    GetStreamRequest,
    NewStreamRequest,
    TerminateConnectionRequest,
)
from cloud_api.lib.conn_worker_client.conn_worker_pb2_grpc import ConnectionWorkerStub
from cloud_api.lib.conn_worker_client.mapping import (
    connection_from_proto,
    stream_from_proto,
)
from cloud_api.models.api import (
    CloseStreamResponse,
    Connection,
    Stream,
    TerminateConnResponse,
)


class ConnWorkerClient:
    """Thin gRPC client. Every method returns cloud_api domain models, never
    raw protobuf messages (see mapping.py)."""

    def __init__(self, target: str, credentials: grpc.ChannelCredentials | None = None):
        self._target = target
        self._credentials = credentials  # None = plaintext (local dev / compose)
        self._channel: grpc.aio.Channel | None = None
        self._stub: ConnectionWorkerStub | None = None

    @property
    def target(self) -> str:
        return self._target

    def _get_stub(self) -> ConnectionWorkerStub:
        """Lazily create the channel and stub on first use (must be inside an async context)."""
        if self._channel is None:
            self._channel = (
                grpc.aio.secure_channel(self._target, self._credentials)
                if self._credentials is not None
                else grpc.aio.insecure_channel(self._target)
            )
            self._stub = ConnectionWorkerStub(self._channel)
        return self._stub  # type: ignore[return-value]

    async def ping(self, timeout: float = 5.0) -> bool:
        """True once the channel is connected (TLS handshake included)."""
        self._get_stub()
        assert self._channel is not None
        try:
            await asyncio.wait_for(self._channel.channel_ready(), timeout)
            return True
        except (TimeoutError, grpc.aio.AioRpcError):
            return False

    async def available(self) -> bool:
        """A statically configured worker is always assumed to be there; the
        on-demand client (service/dynamic_conn_worker_client) overrides this."""
        return True

    async def new_connection(
        self, node_id: str, name: str, id: str | None = None
    ) -> Connection:
        """`id` makes the worker keep the caller's identity for the connection
        (and makes a retry of the same call idempotent)."""
        msg = await self._get_stub().CreateConnection(
            CreateConnectionRequest(node_id=node_id, name=name, id=id or "")
        )
        return connection_from_proto(msg)

    async def get_connection(self, connection_id: str) -> Connection:
        msg = await self._get_stub().GetConnection(
            GetConnectionRequest(id=connection_id)
        )
        return connection_from_proto(msg)

    async def terminate_connection(self, connection_id: str) -> TerminateConnResponse:
        # conn_worker returns Empty on success and a gRPC error otherwise, so
        # reaching the next line *is* success.
        await self._get_stub().TerminateConnection(
            TerminateConnectionRequest(id=connection_id)
        )
        return TerminateConnResponse(id=connection_id, status="succeeded")

    async def list_connections(self) -> list[Connection]:
        result = []
        async for msg in self._get_stub().ListConnections(empty_pb2.Empty()):
            result.append(connection_from_proto(msg))
        return result

    async def new_stream(
        self, connection_id: str, type: str, endpoint: str, port: int
    ) -> Stream:
        msg = await self._get_stub().NewStream(
            NewStreamRequest(
                connection_id=connection_id,
                type=type,
                endpoint=endpoint,
                port=port,
            )
        )
        return stream_from_proto(msg)

    async def close_stream(self, stream_id: str) -> CloseStreamResponse:
        msg = await self._get_stub().CloseStream(
            CloseStreamRequest(stream_id=stream_id)
        )
        return CloseStreamResponse(id=msg.id, status=msg.status)

    async def list_streams(self) -> list[Stream]:
        result = []
        async for msg in self._get_stub().ListStreams(empty_pb2.Empty()):
            result.append(stream_from_proto(msg))
        return result

    async def get_stream(self, stream_id: str) -> Stream:
        msg = await self._get_stub().GetStream(GetStreamRequest(id=stream_id))
        return stream_from_proto(msg)

    async def close(self) -> None:
        if self._channel is not None:
            await self._channel.close()
