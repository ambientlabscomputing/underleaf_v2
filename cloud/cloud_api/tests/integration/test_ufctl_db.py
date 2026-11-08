"""ufctl device login, device keys and the standing remote connection against
real Postgres: the conditional UPDATEs that make codes single use, owner
scoping of revocation, and the /internal/remote-auth path end to end.

Skipped when the database has not been migrated to the ufctl revision."""

import asyncio
from datetime import UTC, datetime, timedelta

import httpx
import pytest
import pytest_asyncio
from sqlalchemy import text

import cloud_api
from cloud_api import service_manager
from cloud_api.interface.router import app
from cloud_api.lib.assertion_lib import AssertionLib
from cloud_api.models.api import Connection, ConnectionState
from cloud_api.models.api.ufctl import UfctlDeviceRequest
from cloud_api.repository import session as db_session
from cloud_api.repository.device_key_repository import DeviceKeyRepository
from cloud_api.repository.device_login_repository import DeviceLoginRepository
from cloud_api.service.assertion_service import AssertionService
from cloud_api.service.device_key_service import DeviceKeyService
from cloud_api.service.device_login_service import DeviceLoginError, DeviceLoginService
from cloud_api.service.remote_auth_service import RemoteAuthService
from cloud_api.service_manager import get_remote_auth_service

BASE = cloud_api.app_config.api.base_path


@pytest_asyncio.fixture
async def ufctl_db(db):
    try:
        async with db_session.get_session() as s:
            await s.execute(text("SELECT 1 FROM underleaf.device_keys LIMIT 1"))
            await s.execute(
                text("SELECT 1 FROM underleaf.device_login_requests LIMIT 1")
            )
    except Exception as exc:  # noqa: BLE001
        pytest.skip(f"ufctl migration not applied: {exc}")


@pytest.fixture
def keys_svc():
    return DeviceKeyService(DeviceKeyRepository())


@pytest.fixture
def login_svc(keys_svc):
    return DeviceLoginService(DeviceLoginRepository(), keys_svc)


async def _poll_code(svc, code, now):
    return await svc.poll(code, now=now)


@pytest.mark.asyncio
async def test_login_flow_against_postgres(ufctl_db, tenant, login_svc, keys_svc):
    dev = await login_svc.start(UfctlDeviceRequest(device_name="it-laptop"))
    t0 = datetime.now(UTC)
    with pytest.raises(DeviceLoginError) as e:
        await login_svc.poll(dev.device_code, now=t0)
    assert e.value.code == "authorization_pending"
    with pytest.raises(DeviceLoginError) as e:
        await login_svc.poll(dev.device_code, now=t0 + timedelta(seconds=1))
    assert e.value.code == "slow_down"

    info = await login_svc.get_request(dev.user_code, tenant.principal_id)
    assert info.device_name == "it-laptop"
    await login_svc.approve(dev.user_code, tenant.principal_id)

    got = await login_svc.poll(dev.device_code, now=t0 + timedelta(seconds=20))
    key = await keys_svc.authenticate(got.device_key)
    assert key is not None and key.id == got.device_key_id
    assert key.principal_account_id == tenant.principal_id
    # stored hashed, never in clear
    async with db_session.get_session() as s:
        stored = (
            await s.execute(
                text("SELECT key_hash FROM underleaf.device_keys WHERE id=:i"),
                {"i": got.device_key_id},
            )
        ).scalar_one()
    assert stored != got.device_key and got.device_key not in stored

    with pytest.raises(DeviceLoginError) as e:
        await login_svc.poll(dev.device_code, now=t0 + timedelta(seconds=40))
    assert e.value.code == "expired_token"


@pytest.mark.asyncio
async def test_racing_polls_mint_one_key(ufctl_db, tenant, login_svc):
    dev = await login_svc.start(UfctlDeviceRequest(device_name="race"))
    await login_svc.approve(dev.user_code, tenant.principal_id)
    t = datetime.now(UTC) + timedelta(seconds=30)
    results = await asyncio.gather(
        *[login_svc.poll(dev.device_code, now=t) for _ in range(8)],
        return_exceptions=True,
    )
    assert sum(not isinstance(r, Exception) for r in results) == 1
    async with db_session.get_session() as s:
        n = (
            await s.execute(
                text(
                    "SELECT count(*) FROM underleaf.device_keys "
                    "WHERE principal_account_id=:p AND name='race'"
                ),
                {"p": tenant.principal_id},
            )
        ).scalar_one()
    assert n == 1


@pytest.mark.asyncio
async def test_expired_request_is_not_decided_or_consumed(ufctl_db, tenant, login_svc):
    dev = await login_svc.start(UfctlDeviceRequest(device_name="old"))
    repo = DeviceLoginRepository()
    row = await repo.get_by_user_code(dev.user_code)
    late = row.expires_at + timedelta(seconds=1)
    assert await repo.decide(row.id, "approved", tenant.principal_id, late) is False
    assert (
        await repo.decide(row.id, "approved", tenant.principal_id, datetime.now(UTC))
        is True
    )
    assert await repo.consume(row.id, late) is False


@pytest.mark.asyncio
async def test_revocation_is_owner_scoped(ufctl_db, tenant, other_tenant, keys_svc):
    key, secret = await keys_svc.create_for_account(tenant.principal_id, "laptop")
    assert await keys_svc.revoke(other_tenant.principal_id, key.id) is False
    assert await keys_svc.authenticate(secret) is not None
    assert [k.id for k in await keys_svc.list_for_account(tenant.principal_id)] == [
        key.id
    ]
    assert await keys_svc.list_for_account(other_tenant.principal_id) == []
    assert await keys_svc.revoke(tenant.principal_id, key.id) is True
    assert await keys_svc.revoke(tenant.principal_id, key.id) is True  # idempotent
    assert await keys_svc.authenticate(secret) is None
    assert await keys_svc.list_for_account(tenant.principal_id) == []


@pytest.mark.asyncio
async def test_last_used_is_recorded_once_per_window(ufctl_db, tenant, keys_svc):
    _key, secret = await keys_svc.create_for_account(tenant.principal_id, "laptop")
    await keys_svc.authenticate(secret)
    first = (await keys_svc.list_for_account(tenant.principal_id))[0].last_used_at
    assert first is not None
    await keys_svc.authenticate(secret)
    assert (await keys_svc.list_for_account(tenant.principal_id))[
        0
    ].last_used_at == first


async def _conn(node_id, name, state, created):
    conn = Connection(name=name, node_id=node_id, state=state)
    await service_manager.get_conn_repo().create_connection(conn)
    async with db_session.get_session() as s:
        await s.execute(
            text("UPDATE underleaf.connections SET created_at=:c WHERE id=:i"),
            {"c": created, "i": conn.id},
        )
        await s.commit()
    return conn.id


@pytest.mark.asyncio
async def test_standing_connection_lookup(ufctl_db, tenant, other_tenant):
    repo = service_manager.get_conn_repo()
    assert await repo.get_standing_remote_connection(tenant.cluster_id) is None
    now = datetime.now(UTC)
    await _conn(tenant.node_id, "remote-access", ConnectionState.PROVISIONED, now)
    await _conn(tenant.node_id, "remote-access", ConnectionState.CLOSED, now)
    await _conn(tenant.node_id, "something-else", ConnectionState.RUNNING, now)
    await _conn(other_tenant.node_id, "remote-access", ConnectionState.RUNNING, now)
    # none of: not running, closed, wrong name, another cluster's
    assert await repo.get_standing_remote_connection(tenant.cluster_id) is None
    old = await _conn(
        tenant.node_id,
        "remote-access",
        ConnectionState.RUNNING,
        now - timedelta(hours=2),
    )
    got = await repo.get_standing_remote_connection(tenant.cluster_id)
    assert got is not None and got.id == old
    new = await _conn(tenant.node_id, "remote-access", ConnectionState.RUNNING, now)
    got = await repo.get_standing_remote_connection(tenant.cluster_id)
    assert got is not None and got.id == new  # the newest wins
    other = await repo.get_standing_remote_connection(other_tenant.cluster_id)
    assert other is not None and other.id != new
    assert await repo.get_standing_remote_connection("cluster_missing") is None


@pytest.mark.asyncio
async def test_remote_auth_end_to_end(
    ufctl_db, tenant, other_tenant, keys_svc, tmp_path
):
    svc = RemoteAuthService(
        auth_lib=service_manager.get_auth_lib(),
        device_key_service=keys_svc,
        assertion_service=AssertionService(
            AssertionLib(str(tmp_path / "keys")), service_manager.get_cluster_repo()
        ),
        conn_repo=service_manager.get_conn_repo(),
    )
    app.dependency_overrides[get_remote_auth_service] = lambda: svc
    try:
        _, secret = await keys_svc.create_for_account(tenant.principal_id, "laptop")
        conn_id = await _conn(
            tenant.node_id, "remote-access", ConnectionState.RUNNING, datetime.now(UTC)
        )
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=app), base_url="http://test"
        ) as c:

            async def call(cluster, credential=secret, **h):
                return await c.get(
                    f"{BASE}/internal/remote-auth",
                    headers={
                        "Authorization": f"Bearer {credential}",
                        "X-Underleaf-Cluster": cluster,
                        **h,
                    },
                )

            ok = await call(tenant.cluster_id)
            assert ok.status_code == 200 and ok.content == b""
            assert ok.headers["x-underleaf-connection"] == conn_id
            assert ok.headers["x-underleaf-assertion"]

            foreign = await call(other_tenant.cluster_id)
            missing = await call("cluster_does_not_exist")
            assert foreign.status_code == missing.status_code == 403
            assert foreign.content == missing.content == b""
            assert "x-underleaf-assertion" not in foreign.headers

            # the other tenant's cluster has no connection of ours, and a
            # subject-id header is refused
            assert (
                await call(tenant.cluster_id, **{"X-Subject-Id": tenant.cluster_id})
            ).status_code == 400

            # revoked key: 401
            (key,) = await keys_svc.list_for_account(tenant.principal_id)
            await keys_svc.revoke(tenant.principal_id, key.id)
            assert (await call(tenant.cluster_id)).status_code == 401
    finally:
        app.dependency_overrides.pop(get_remote_auth_service, None)
