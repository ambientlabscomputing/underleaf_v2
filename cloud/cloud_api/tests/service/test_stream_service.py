"""Guard-rail unit tests (mocked collaborators) for StreamService.

The lifecycle and metering/enforcement flows run against a real DB in
tests/integration; these pin the branches that are awkward to reach there.
"""

from unittest.mock import AsyncMock

import pytest

from cloud_api.models.api import BillingAccount, Connection, NewStreamRequest, Stream
from cloud_api.service.stream_service import (
    InsufficientNetworkTrafficBalanceError,
    StreamService,
)


def _service(**overrides):
    deps = {
        "stream_repo": AsyncMock(),
        "conn_worker_client": AsyncMock(),
        "conn_repo": AsyncMock(),
        "billing_account_repo": AsyncMock(),
        "entitlements_bucket_service": AsyncMock(),
        "usage_event_service": AsyncMock(),
        "usage_enforcement_service": AsyncMock(),
    }
    deps.update(overrides)
    return StreamService(**deps), deps


def _account() -> BillingAccount:
    return BillingAccount(id="billing_1", name="A", principal_account_id="principal_1")


def _stream(**kw) -> Stream:
    defaults = {
        "id": "stream_1",
        "name": "s",
        "connection_id": "conn_1",
        "type": "gateway",
    }
    defaults.update(kw)
    return Stream(**defaults)


def _wire_account(deps):
    deps["conn_repo"].get_connection.return_value = Connection(
        id="conn_1", name="c", node_id="node_1"
    )
    deps[
        "billing_account_repo"
    ].get_billing_account_by_node_id.return_value = _account()


@pytest.mark.asyncio
async def test_create_stream_is_refused_when_out_of_data():
    svc, deps = _service()
    _wire_account(deps)
    deps["entitlements_bucket_service"].has_network_traffic_balance.return_value = False

    with pytest.raises(InsufficientNetworkTrafficBalanceError):
        await svc.create_stream(
            NewStreamRequest(connection_id="conn_1", type="gateway")
        )

    deps["conn_worker_client"].new_stream.assert_not_awaited()


@pytest.mark.asyncio
async def test_create_stream_fails_open_when_account_unresolvable():
    svc, deps = _service()
    deps["conn_repo"].get_connection.return_value = None
    deps["conn_worker_client"].new_stream.return_value = _stream()
    deps["stream_repo"].create_stream.return_value = _stream()

    await svc.create_stream(NewStreamRequest(connection_id="conn_1", type="gateway"))

    deps["entitlements_bucket_service"].has_network_traffic_balance.assert_not_awaited()


def _worker_stream(total):
    s = _stream()
    s.bytes_in = total
    return s


@pytest.mark.asyncio
async def test_replayed_usage_event_is_not_charged_again():
    svc, deps = _service()
    _wire_account(deps)
    deps["conn_worker_client"].list_streams.return_value = [_worker_stream(300)]
    deps["stream_repo"].get_stream.return_value = _stream(last_reported_bytes=100)
    # idempotency key already seen -> created=False
    deps["usage_event_service"].record_usage.return_value = (AsyncMock(), False)

    await svc.sync_usage_from_conn_worker()

    deps["usage_enforcement_service"].record_traffic.assert_not_awaited()
    deps["stream_repo"].update_last_reported_bytes.assert_awaited_once_with(
        "stream_1", 300
    )


@pytest.mark.asyncio
async def test_new_usage_is_charged_once_with_the_delta():
    svc, deps = _service()
    _wire_account(deps)
    deps["conn_worker_client"].list_streams.return_value = [_worker_stream(300)]
    deps["stream_repo"].get_stream.return_value = _stream(last_reported_bytes=100)
    deps["usage_event_service"].record_usage.return_value = (AsyncMock(), True)
    deps["usage_enforcement_service"].record_traffic.return_value = False

    await svc.sync_usage_from_conn_worker()

    kwargs = deps["usage_event_service"].record_usage.await_args.kwargs
    assert kwargs["usage_amount"] == 200
    assert kwargs["idempotency_key"] == "stream_1:100:300"
    deps["usage_enforcement_service"].record_traffic.assert_awaited_once_with(
        "billing_1", 200
    )


@pytest.mark.asyncio
@pytest.mark.parametrize("worker_total,local_last", [(0, 0), (100, 100), (50, 100)])
async def test_no_new_bytes_means_no_usage_event(worker_total, local_last):
    svc, deps = _service()
    deps["conn_worker_client"].list_streams.return_value = [
        _worker_stream(worker_total)
    ]
    deps["stream_repo"].get_stream.return_value = _stream(
        last_reported_bytes=local_last
    )

    await svc.sync_usage_from_conn_worker()

    deps["usage_event_service"].record_usage.assert_not_awaited()


@pytest.mark.asyncio
async def test_stream_unknown_locally_or_without_account_is_not_billed():
    svc, deps = _service()
    deps["conn_worker_client"].list_streams.return_value = [_worker_stream(300)]
    deps["stream_repo"].get_stream.return_value = None
    await svc.sync_usage_from_conn_worker()
    deps["usage_event_service"].record_usage.assert_not_awaited()

    deps["stream_repo"].get_stream.return_value = _stream(last_reported_bytes=0)
    deps["conn_repo"].get_connection.return_value = None
    await svc.sync_usage_from_conn_worker()
    deps["usage_event_service"].record_usage.assert_not_awaited()


@pytest.mark.asyncio
async def test_hard_stop_closes_every_active_stream_once_per_account():
    svc, deps = _service()
    _wire_account(deps)
    deps["conn_worker_client"].list_streams.return_value = [_worker_stream(300)]
    deps["stream_repo"].get_stream.return_value = _stream(last_reported_bytes=100)
    deps["usage_event_service"].record_usage.return_value = (AsyncMock(), True)
    deps["usage_enforcement_service"].record_traffic.return_value = True  # hard stop
    deps["stream_repo"].query_streams.return_value = [
        _stream(id="s1"),
        _stream(id="s2"),
    ]
    svc.close_stream = AsyncMock()

    await svc.sync_usage_from_conn_worker()

    assert [c.args[0] for c in svc.close_stream.await_args_list] == ["s1", "s2"]
