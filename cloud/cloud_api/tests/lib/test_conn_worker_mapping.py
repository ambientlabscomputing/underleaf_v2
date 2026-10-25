"""conn_worker's wire format quirks, via real protobuf messages."""

from datetime import UTC, datetime

from cloud_api.lib.conn_worker_client import conn_worker_pb2 as pb2
from cloud_api.lib.conn_worker_client.mapping import (
    connection_from_proto,
    stream_from_proto,
)
from cloud_api.models.api import ConnectionState, Status, StreamState, StreamType


def test_stream_with_all_optionals_empty_maps_to_valid_domain_model():
    msg = pb2.Stream(
        id="stream_1",
        connection_id="conn_1",
        type="gateway",
        state="active",
        status="succeeded",
    )  # port 0, endpoint "", created_at "", closed_at "" -- as sent by the Go side

    stream = stream_from_proto(msg)

    assert stream.id == "stream_1"
    assert stream.name == "stream_1"  # protobuf Stream has no name
    assert stream.type_ == StreamType.GW
    assert stream.state == StreamState.ACTIVE and stream.status == Status.SUCCEEDED
    assert stream.port is None and stream.endpoint is None
    assert stream.closed_at is None
    assert stream.created_at.tzinfo is not None


def test_stream_carries_transient_byte_counters_but_never_serializes_them():
    stream = stream_from_proto(
        pb2.Stream(id="s", connection_id="c", type="gateway", bytes_in=7, bytes_out=5)
    )

    assert stream.bytes_in + stream.bytes_out == 12
    dumped = stream.model_dump(by_alias=True)
    assert "bytes_in" not in dumped and "bytes_out" not in dumped
    assert dumped["type"] == "gateway"  # DB column name, not the python alias


def test_stream_parses_rfc3339_timestamps_and_port():
    stream = stream_from_proto(
        pb2.Stream(
            id="s",
            connection_id="c",
            type="gateway",
            state="closed",
            port=8080,
            endpoint="e",
            created_at="2026-10-24T10:00:00Z",
            closed_at="2026-10-24T11:30:00Z",
        )
    )

    assert stream.port == 8080 and stream.endpoint == "e"
    assert stream.created_at == datetime(2026, 10, 24, 10, tzinfo=UTC)
    assert stream.closed_at == datetime(2026, 10, 24, 11, 30, tzinfo=UTC)


def test_connection_with_empty_timestamps_maps_to_valid_domain_model():
    conn = connection_from_proto(
        pb2.Connection(
            id="conn_1", node_id="node_1", name="c", state="running", status="succeeded"
        )
    )

    assert conn.state == ConnectionState.RUNNING
    assert conn.closed_at is None  # the original crash: "" fed to timestamptz
    assert conn.created_at.tzinfo is not None
