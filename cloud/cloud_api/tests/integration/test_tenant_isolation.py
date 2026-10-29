"""Connection/stream endpoints must only act on resources owned by the
caller's tenant (claims.azp). A foreign resource is reported as 404, not 403,
so IDs can't be probed -- and creating against someone else's node/connection
must not touch their billing allowance."""

import httpx
import pytest
import pytest_asyncio

import cloud_api
from cloud_api.interface.router import app
from cloud_api.models.api import (
    ConnectionState,
    CreateConnectionRequest,
    NewStreamRequest,
    StreamState,
)

BASE = cloud_api.app_config.api.base_path


def _client(wired_services, principal_id: str) -> httpx.AsyncClient:
    token = wired_services.get_auth_lib().mint_access_token(
        user_id=f"user_{principal_id}", principal_account_id=principal_id
    )
    return httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app),
        base_url=f"http://test{BASE}",
        headers={"Authorization": f"Bearer {token}"},
    )


@pytest_asyncio.fixture
async def victim_conn(wired_services, tenant):
    return await wired_services.get_connection_service().create_connection(
        CreateConnectionRequest(name="victim", node_id=tenant.node_id)
    )


@pytest_asyncio.fixture
async def victim_stream(wired_services, victim_conn):
    return await wired_services.get_stream_service().create_stream(
        NewStreamRequest(connection_id=victim_conn.id, type="gateway", port=8080)
    )


@pytest_asyncio.fixture
async def attacker(wired_services, other_tenant):
    async with _client(wired_services, other_tenant.principal_id) as c:
        yield c


@pytest_asyncio.fixture
async def owner(wired_services, tenant):
    async with _client(wired_services, tenant.principal_id) as c:
        yield c


async def _slots(wired_services, tenant) -> int:
    bucket = await wired_services.get_entitlements_bucket_repo().get_entitlements_bucket_by_billing_account(
        tenant.billing_account_id
    )
    return bucket.connection_slot_balance


# --- connections -----------------------------------------------------------


@pytest.mark.asyncio
async def test_foreign_connection_is_404_for_every_verb(
    wired_services, attacker, victim_conn
):
    cid = victim_conn.id
    responses = [
        await attacker.get(f"/connections/{cid}"),
        await attacker.patch(f"/connections/{cid}", json={"name": "pwned"}),
        await attacker.post(f"/connections/{cid}/terminate"),
        await attacker.delete(f"/connections/{cid}"),
    ]
    assert [r.status_code for r in responses] == [404] * 4
    # Same body as a truly missing id, so existence can't be inferred.
    missing = await attacker.get("/connections/conn_does_not_exist")
    assert missing.status_code == 404
    assert all(r.json() == missing.json() for r in responses)

    untouched = await wired_services.get_conn_repo().get_connection(cid)
    assert untouched.name == "victim"
    assert untouched.state == ConnectionState.PROVISIONED


@pytest.mark.asyncio
async def test_foreign_closed_connection_cannot_be_deleted(
    wired_services, attacker, victim_conn
):
    await wired_services.get_connection_service().terminate_connection(victim_conn.id)

    resp = await attacker.delete(f"/connections/{victim_conn.id}")

    assert resp.status_code == 404
    assert await wired_services.get_conn_repo().get_connection(victim_conn.id)


@pytest.mark.asyncio
async def test_owner_can_still_manage_own_connection(owner, victim_conn):
    assert (await owner.get(f"/connections/{victim_conn.id}")).status_code == 200
    patched = await owner.patch(f"/connections/{victim_conn.id}", json={"name": "new"})
    assert patched.status_code == 200 and patched.json()["name"] == "new"
    terminated = await owner.post(f"/connections/{victim_conn.id}/terminate")
    assert terminated.status_code == 200
    assert (await owner.delete(f"/connections/{victim_conn.id}")).status_code == 204


@pytest.mark.asyncio
async def test_cannot_create_connection_on_foreign_node(
    wired_services, attacker, tenant, fake_stub
):
    slots_before = await _slots(wired_services, tenant)

    resp = await attacker.post(
        "/connections", json={"name": "leech", "node_id": tenant.node_id}
    )

    assert resp.status_code == 404
    assert not fake_stub.connections
    assert await _slots(wired_services, tenant) == slots_before


@pytest.mark.asyncio
async def test_cannot_create_connection_on_unknown_node(attacker, fake_stub):
    resp = await attacker.post(
        "/connections", json={"name": "x", "node_id": "node_does_not_exist"}
    )
    assert resp.status_code == 404
    assert not fake_stub.connections


@pytest.mark.asyncio
async def test_owner_can_create_connection_on_own_node(owner, tenant):
    resp = await owner.post(
        "/connections", json={"name": "mine", "node_id": tenant.node_id}
    )
    assert resp.status_code == 201


# --- streams ---------------------------------------------------------------


@pytest.mark.asyncio
async def test_foreign_stream_is_404_for_every_verb(
    wired_services, attacker, victim_stream
):
    sid = victim_stream.id
    responses = [
        await attacker.get(f"/streams/{sid}"),
        await attacker.patch(f"/streams/{sid}", json={"name": "pwned"}),
        await attacker.post(f"/streams/{sid}/close"),
        await attacker.delete(f"/streams/{sid}"),
    ]
    assert [r.status_code for r in responses] == [404] * 4
    missing = await attacker.get("/streams/stream_does_not_exist")
    assert missing.status_code == 404
    assert all(r.json() == missing.json() for r in responses)

    untouched = await wired_services.get_stream_repo().get_stream(sid)
    assert untouched.state == StreamState.ACTIVE
    assert untouched.name == victim_stream.name


@pytest.mark.asyncio
async def test_foreign_closed_stream_cannot_be_deleted(
    wired_services, attacker, victim_stream
):
    await wired_services.get_stream_service().close_stream(victim_stream.id)

    resp = await attacker.delete(f"/streams/{victim_stream.id}")

    assert resp.status_code == 404
    assert await wired_services.get_stream_repo().get_stream(victim_stream.id)


@pytest.mark.asyncio
async def test_owner_can_still_manage_own_stream(owner, victim_stream):
    assert (await owner.get(f"/streams/{victim_stream.id}")).status_code == 200
    patched = await owner.patch(f"/streams/{victim_stream.id}", json={"name": "new"})
    assert patched.status_code == 200
    closed = await owner.post(f"/streams/{victim_stream.id}/close")
    assert closed.status_code == 200
    assert (await owner.delete(f"/streams/{victim_stream.id}")).status_code == 204


@pytest.mark.asyncio
async def test_cannot_create_stream_on_foreign_connection(
    attacker, victim_conn, fake_stub
):
    resp = await attacker.post(
        "/streams",
        json={"connection_id": victim_conn.id, "type": "gateway", "port": 9},
    )

    assert resp.status_code == 404
    assert not fake_stub.streams


@pytest.mark.asyncio
async def test_cannot_create_stream_on_unknown_connection(attacker, fake_stub):
    resp = await attacker.post(
        "/streams",
        json={"connection_id": "conn_does_not_exist", "type": "gateway", "port": 9},
    )
    assert resp.status_code == 404
    assert not fake_stub.streams


@pytest.mark.asyncio
async def test_owner_can_create_stream_on_own_connection(owner, victim_conn):
    resp = await owner.post(
        "/streams",
        json={"connection_id": victim_conn.id, "type": "gateway", "port": 9},
    )
    assert resp.status_code == 201
