"""Regression tests for the connection/stream lifecycle against a real DB and
the real ConnWorkerClient (fake gRPC stub). These cover bugs that mock-based
tests could not see: raw protobuf messages being treated as pydantic models,
local IDs diverging from conn_worker IDs, PATCH silently ignoring `state`,
terminate_connection reading `.status` off None, and query filters passing
kwargs to Select.filter()."""

import pytest

from cloud_api.models.api import (
    ConnectionState,
    CreateConnectionRequest,
    NewStreamRequest,
    QueryStreamRequest,
    StreamState,
)


@pytest.mark.asyncio
async def test_connection_lifecycle_persists_and_closes(
    wired_services, tenant, fake_stub
):
    svc = wired_services.get_connection_service()

    conn = await svc.create_connection(
        CreateConnectionRequest(name="c1", node_id=tenant.node_id)
    )

    # Local row must carry conn_worker's id, or sync could never match them.
    assert conn.id in fake_stub.connections
    assert conn.state == ConnectionState.PROVISIONED

    closed = await svc.terminate_connection(conn.id)

    assert closed.state == ConnectionState.CLOSED
    reread = await svc.conn_repo.get_connection(conn.id)
    assert reread.state == ConnectionState.CLOSED


@pytest.mark.asyncio
async def test_stream_lifecycle_uses_worker_id_and_closes(
    wired_services, tenant, fake_stub
):
    conn_svc = wired_services.get_connection_service()
    stream_svc = wired_services.get_stream_service()
    conn = await conn_svc.create_connection(
        CreateConnectionRequest(name="c1", node_id=tenant.node_id)
    )

    stream = await stream_svc.create_stream(
        NewStreamRequest(connection_id=conn.id, type="gateway", port=8080)
    )

    assert stream.id in fake_stub.streams
    assert stream.state == StreamState.ACTIVE
    assert stream.port == 8080

    closed = await stream_svc.close_stream(stream.id)
    assert closed.state == StreamState.CLOSED
    assert (await stream_svc.stream_repo.get_stream(stream.id)).state == (
        StreamState.CLOSED
    )


@pytest.mark.asyncio
async def test_query_streams_supports_state_filter(wired_services, tenant):
    conn_svc = wired_services.get_connection_service()
    stream_svc = wired_services.get_stream_service()
    conn = await conn_svc.create_connection(
        CreateConnectionRequest(name="c1", node_id=tenant.node_id)
    )
    await stream_svc.create_stream(
        NewStreamRequest(connection_id=conn.id, type="gateway", port=8080)
    )

    result = await stream_svc.get_streams(
        QueryStreamRequest(
            principal_account_id=tenant.principal_id, state=StreamState.ACTIVE
        )
    )

    assert result.total == 1


@pytest.mark.asyncio
async def test_sync_adopts_unknown_worker_streams(wired_services, tenant, fake_stub):
    """A stream that exists only in conn_worker (e.g. created via the
    agent's own RPC) must be imported by the sync."""
    conn_svc = wired_services.get_connection_service()
    stream_svc = wired_services.get_stream_service()
    conn = await conn_svc.create_connection(
        CreateConnectionRequest(name="c1", node_id=tenant.node_id)
    )
    worker_stream = await fake_stub.NewStream(
        type(
            "R",
            (),
            {"connection_id": conn.id, "type": "gateway", "port": 9, "endpoint": ""},
        )()
    )

    await stream_svc._sync_streams()

    local = await stream_svc.stream_repo.get_stream(worker_stream.id)
    assert local is not None
    assert local.connection_id == conn.id
