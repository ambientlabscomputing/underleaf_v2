"""conn_worker protobuf messages -> cloud_api domain models.

conn_worker's wire format is looser than our domain models: optionals arrive
as "" / 0 rather than absent, timestamps are RFC 3339 strings that may be
empty, and the Stream message carries no `name`. Everything that crosses the
gRPC boundary goes through here so the rest of cloud_api only ever sees
validated pydantic models.
"""

from datetime import UTC, datetime

from cloud_api.lib.conn_worker_client import conn_worker_pb2 as pb2
from cloud_api.models.api import (
    Connection,
    ConnectionState,
    Status,
    Stream,
    StreamState,
    StreamType,
)


def _parse_ts(value: str) -> datetime | None:
    if not value:
        return None
    return datetime.fromisoformat(value)


def connection_from_proto(msg: pb2.Connection) -> Connection:
    created_at = _parse_ts(msg.created_at) or datetime.now(UTC)
    return Connection(
        id=msg.id,
        name=msg.name,
        node_id=msg.node_id,
        state=ConnectionState(msg.state) if msg.state else ConnectionState.PROVISIONED,
        status=Status(msg.status) if msg.status else Status.IN_PROGRESS,
        closed_at=_parse_ts(msg.closed_at),
        created_at=created_at,
        updated_at=created_at,
    )


def stream_from_proto(msg: pb2.Stream) -> Stream:
    created_at = _parse_ts(msg.created_at) or datetime.now(UTC)
    return Stream(
        id=msg.id,
        # conn_worker streams are unnamed; the id is the only stable handle.
        name=msg.id,
        connection_id=msg.connection_id,
        type=StreamType(msg.type),
        state=StreamState(msg.state) if msg.state else StreamState.ACTIVE,
        status=Status(msg.status) if msg.status else Status.IN_PROGRESS,
        endpoint=msg.endpoint or None,
        port=msg.port or None,
        closed_at=_parse_ts(msg.closed_at),
        created_at=created_at,
        updated_at=created_at,
        bytes_in=msg.bytes_in,
        bytes_out=msg.bytes_out,
    )
