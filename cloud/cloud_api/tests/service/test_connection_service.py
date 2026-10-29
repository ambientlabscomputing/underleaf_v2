from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from cloud_api.models.api import (
    BillingAccount,
    ConnectionState,
    CreateConnectionRequest,
)
from cloud_api.service.connection_service import ConnectionService


def _make_service(**overrides):
    deps = {
        "conn_repo": AsyncMock(),
        "conn_client": AsyncMock(),
        "billing_account_repo": AsyncMock(),
        "entitlements_bucket_service": AsyncMock(),
    }
    deps.update(overrides)
    return ConnectionService(**deps), deps


def _make_billing_account(**overrides) -> BillingAccount:
    defaults = {
        "id": "billing_1",
        "name": "Acme",
        "principal_account_id": "principal_1",
    }
    defaults.update(overrides)
    return BillingAccount(**defaults)


@pytest.mark.asyncio
async def test_create_connection_reserves_slot_before_calling_conn_worker():
    service, deps = _make_service()
    deps[
        "billing_account_repo"
    ].get_billing_account_by_node_id.return_value = _make_billing_account()
    deps["conn_client"].new_connection.return_value = SimpleNamespace(
        id="conn_1", node_id="node_1", name="c"
    )

    await service.create_connection(CreateConnectionRequest(name="c", node_id="node_1"))

    deps[
        "entitlements_bucket_service"
    ].check_and_reserve_connection_slot.assert_awaited_once_with("billing_1")
    deps["conn_client"].new_connection.assert_awaited_once()


@pytest.mark.asyncio
async def test_create_connection_exhausted_slot_blocks_conn_worker_call():
    service, deps = _make_service()
    deps[
        "billing_account_repo"
    ].get_billing_account_by_node_id.return_value = _make_billing_account()
    deps[
        "entitlements_bucket_service"
    ].check_and_reserve_connection_slot.side_effect = RuntimeError("exhausted")

    with pytest.raises(RuntimeError):
        await service.create_connection(
            CreateConnectionRequest(name="c", node_id="node_1")
        )

    deps["conn_client"].new_connection.assert_not_awaited()


@pytest.mark.asyncio
async def test_create_connection_allowed_when_billing_account_unresolvable():
    service, deps = _make_service()
    deps["billing_account_repo"].get_billing_account_by_node_id.return_value = None
    deps["conn_client"].new_connection.return_value = SimpleNamespace(
        id="conn_1", node_id="node_1", name="c"
    )

    await service.create_connection(CreateConnectionRequest(name="c", node_id="node_1"))

    deps[
        "entitlements_bucket_service"
    ].check_and_reserve_connection_slot.assert_not_awaited()
    deps["conn_client"].new_connection.assert_awaited_once()


@pytest.mark.asyncio
async def test_terminate_connection_releases_slot():
    service, deps = _make_service()
    open_connection = SimpleNamespace(
        id="conn_1", node_id="node_1", state=ConnectionState.PROVISIONED
    )
    closed_connection = SimpleNamespace(
        id="conn_1", node_id="node_1", state=ConnectionState.CLOSED
    )
    deps["conn_repo"].get_connection.return_value = open_connection
    deps["conn_repo"].patch_connection.return_value = closed_connection
    deps["conn_client"].terminate_connection.return_value = SimpleNamespace(
        status="succeeded"
    )
    deps[
        "billing_account_repo"
    ].get_billing_account_by_node_id.return_value = _make_billing_account()

    await service.terminate_connection("conn_1")

    deps[
        "entitlements_bucket_service"
    ].release_connection_slot.assert_awaited_once_with("billing_1")
