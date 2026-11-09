"""/ufctl: the device login endpoints (wire contract: edge/cmd/ufctl/
remoteaccess/doc.go) and the owner's device key management."""

import pytest
from cryptography.hazmat.primitives.asymmetric import rsa
from fastapi.testclient import TestClient

import cloud_api
from cloud_api.interface.router import app
from cloud_api.lib.auth_lib import AuthLib
from cloud_api.service.device_key_service import DeviceKeyService
from cloud_api.service.device_login_service import DeviceLoginService
from cloud_api.service_manager import (
    get_auth_lib,
    get_device_key_service,
    get_device_login_service,
)
from tests.ufctl_fakes import FakeDeviceKeyRepo, FakeDeviceLoginRepo

BASE = f"{cloud_api.app_config.api.base_path}/ufctl"
GRANT = "urn:ietf:params:oauth:grant-type:device_code"
ALICE, BOB = "principal_alice", "principal_bob"


@pytest.fixture
def env():
    auth_lib = AuthLib.__new__(AuthLib)
    auth_lib._private_key = rsa.generate_private_key(
        public_exponent=65537, key_size=2048
    )
    auth_lib._public_key = auth_lib._private_key.public_key()
    keys = FakeDeviceKeyRepo()
    logins = FakeDeviceLoginRepo()
    key_svc = DeviceKeyService(keys)
    login_svc = DeviceLoginService(logins, key_svc)
    app.dependency_overrides[get_auth_lib] = lambda: auth_lib
    app.dependency_overrides[get_device_key_service] = lambda: key_svc
    app.dependency_overrides[get_device_login_service] = lambda: login_svc
    try:
        yield type(
            "Env",
            (),
            {
                "client": TestClient(app),
                "auth_lib": auth_lib,
                "keys": keys,
                "logins": logins,
                "key_svc": key_svc,
                "login_svc": login_svc,
            },
        )
    finally:
        for dep in (get_auth_lib, get_device_key_service, get_device_login_service):
            app.dependency_overrides.pop(dep, None)


def _as(env, account, user=None):
    token = env.auth_lib.mint_access_token(
        user_id=user or f"user_{account}", principal_account_id=account
    )
    return {"Authorization": f"Bearer {token}"}


def _start(env, name="my-laptop"):
    r = env.client.post(f"{BASE}/device", json={"device_name": name})
    assert r.status_code == 200, r.text
    return r.json()


def _poll(env, device_code, grant=GRANT):
    return env.client.post(
        f"{BASE}/token", json={"grant_type": grant, "device_code": device_code}
    )


def _forget_poll_clock(env):
    for row in env.logins.rows.values():
        row.last_polled_at = None


# ---------------------------------------------------- the ufctl-facing contract


def test_device_endpoint_returns_exactly_the_documented_fields(env):
    body = _start(env)
    assert set(body) == {
        "device_code",
        "user_code",
        "verification_uri",
        "verification_uri_complete",
        "expires_in",
        "interval",
    }
    assert body["verification_uri"].endswith("/ufctl/activate")
    assert body["verification_uri_complete"].endswith(
        f"/ufctl/activate?user_code={body['user_code']}"
    )
    assert isinstance(body["expires_in"], int) and isinstance(body["interval"], int)
    assert body["interval"] > 0 and body["expires_in"] > 0


def test_device_endpoint_needs_no_authentication_and_is_not_cacheable(env):
    r = env.client.post(f"{BASE}/device", json={"device_name": "x"})
    assert r.status_code == 200
    assert r.headers["cache-control"] == "no-store"


def test_device_name_may_be_empty_or_missing(env):
    assert (
        env.client.post(f"{BASE}/device", json={"device_name": ""}).status_code == 200
    )
    assert env.client.post(f"{BASE}/device", json={}).status_code == 200


def test_token_errors_are_top_level_error_bodies_with_status_400(env):
    """The Go client reads {"error": ...} from the body of a 400. The usual
    {"detail": ...} envelope would be an unparseable login for it."""
    dev = _start(env)
    r = _poll(env, dev["device_code"])
    assert r.status_code == 400
    assert r.json() == {"error": "authorization_pending"}
    assert r.headers["cache-control"] == "no-store"
    r = _poll(env, dev["device_code"])  # immediately again
    assert r.status_code == 400 and r.json() == {"error": "slow_down"}
    assert _poll(env, "0" * 64).json() == {"error": "expired_token"}
    assert _poll(env, dev["device_code"], grant="password").json() == {
        "error": "unsupported_grant_type"
    }


def test_the_whole_flow_as_ufctl_and_the_owner_drive_it(env):
    dev = _start(env, "work-laptop")
    assert _poll(env, dev["device_code"]).json() == {"error": "authorization_pending"}

    info = env.client.get(
        f"{BASE}/device/request",
        params={"user_code": dev["user_code"]},
        headers=_as(env, ALICE),
    )
    assert info.status_code == 200
    assert info.json()["device_name"] == "work-laptop"
    assert info.json()["status"] == "pending"

    ok = env.client.post(
        f"{BASE}/device/approve",
        json={"user_code": dev["user_code"]},
        headers=_as(env, ALICE),
    )
    assert ok.status_code == 200 and ok.json()["status"] == "approved"

    _forget_poll_clock(env)
    r = _poll(env, dev["device_code"])
    assert r.status_code == 200
    body = r.json()
    assert set(body) == {"device_key_id", "device_key"}
    assert body["device_key"].startswith("ufdk_") and body["device_key_id"].startswith(
        "dk_"
    )
    assert r.headers["cache-control"] == "no-store"

    # the key is bound to the approving account, and the code is now spent
    (row,) = env.keys.rows.values()
    assert row.principal_account_id == ALICE and row.id == body["device_key_id"]
    _forget_poll_clock(env)
    assert _poll(env, dev["device_code"]).json() == {"error": "expired_token"}


def test_denied_login_is_access_denied_and_makes_no_key(env):
    dev = _start(env)
    r = env.client.post(
        f"{BASE}/device/deny",
        json={"user_code": dev["user_code"]},
        headers=_as(env, ALICE),
    )
    assert r.status_code == 200 and r.json()["status"] == "denied"
    assert _poll(env, dev["device_code"]).json() == {"error": "access_denied"}
    assert env.keys.rows == {}


# ------------------------------------------------ account side needs a person


@pytest.mark.parametrize(
    "method,path",
    [
        ("get", "/device/request?user_code=ABCD-EFGH"),
        ("post", "/device/approve"),
        ("post", "/device/deny"),
        ("get", "/devices"),
        ("delete", "/devices/dk_x"),
    ],
)
def test_account_endpoints_reject_anonymous_callers(env, method, path):
    kwargs = {"json": {"user_code": "ABCD-EFGH"}} if method == "post" else {}
    r = getattr(env.client, method)(f"{BASE}{path}", **kwargs)
    assert r.status_code in (401, 403)


def test_refresh_token_cannot_approve(env):
    dev = _start(env)
    refresh = env.auth_lib.mint_refresh_token("user_alice")
    r = env.client.post(
        f"{BASE}/device/approve",
        json={"user_code": dev["user_code"]},
        headers={"Authorization": f"Bearer {refresh}"},
    )
    assert r.status_code == 403
    assert next(iter(env.logins.rows.values())).status == "pending"


def test_second_decision_is_409_and_cannot_rebind_the_account(env):
    dev = _start(env)
    body = {"user_code": dev["user_code"]}
    assert (
        env.client.post(
            f"{BASE}/device/approve", json=body, headers=_as(env, ALICE)
        ).status_code
        == 200
    )
    r = env.client.post(f"{BASE}/device/approve", json=body, headers=_as(env, BOB))
    assert r.status_code == 409
    assert next(iter(env.logins.rows.values())).principal_account_id == ALICE


def test_unknown_user_code_is_404_and_guessing_is_rate_limited(env):
    h = _as(env, ALICE)
    for _ in range(10):
        r = env.client.post(
            f"{BASE}/device/approve", json={"user_code": "ZZZZ-ZZZZ"}, headers=h
        )
        assert r.status_code == 404
    r = env.client.post(
        f"{BASE}/device/approve", json={"user_code": "ZZZZ-ZZZZ"}, headers=h
    )
    assert r.status_code == 429


# ------------------------------------------------------------ device keys


@pytest.mark.asyncio
async def test_owner_lists_only_their_own_keys_without_secrets(env):
    a, secret_a = await env.key_svc.create_for_account(ALICE, "alice-laptop")
    await env.key_svc.create_for_account(BOB, "bob-laptop")
    r = env.client.get(f"{BASE}/devices", headers=_as(env, ALICE))
    assert r.status_code == 200
    body = r.json()
    assert body["total"] == 1
    (item,) = body["items"]
    assert item["id"] == a.id and item["name"] == "alice-laptop"
    assert item["last_used_at"] is None and item["revoked_at"] is None
    assert secret_a not in r.text and "key_hash" not in r.text and "ufdk_" not in r.text


@pytest.mark.asyncio
async def test_owner_revokes_their_key_and_it_stops_working(env):
    key, secret = await env.key_svc.create_for_account(ALICE, "laptop")
    r = env.client.delete(f"{BASE}/devices/{key.id}", headers=_as(env, ALICE))
    assert r.status_code == 204 and r.content == b""
    assert await env.key_svc.authenticate(secret) is None
    assert (
        env.client.get(f"{BASE}/devices", headers=_as(env, ALICE)).json()["items"] == []
    )


@pytest.mark.asyncio
async def test_foreign_and_missing_ids_are_both_404_and_nothing_is_revoked(env):
    key, secret = await env.key_svc.create_for_account(ALICE, "laptop")
    foreign = env.client.delete(f"{BASE}/devices/{key.id}", headers=_as(env, BOB))
    missing = env.client.delete(f"{BASE}/devices/dk_missing", headers=_as(env, BOB))
    assert foreign.status_code == missing.status_code == 404
    assert foreign.json() == missing.json()
    assert await env.key_svc.authenticate(secret) is not None


def test_routes_are_registered_under_the_api_base():
    routes = {
        (m, r.path) for r in app.routes if hasattr(r, "methods") for m in r.methods
    }
    for method, suffix in [
        ("POST", "/device"),
        ("POST", "/token"),
        ("GET", "/device/request"),
        ("POST", "/device/approve"),
        ("POST", "/device/deny"),
        ("GET", "/devices"),
        ("DELETE", "/devices/{device_key_id}"),
    ]:
        assert (method, f"{BASE}{suffix}") in routes
