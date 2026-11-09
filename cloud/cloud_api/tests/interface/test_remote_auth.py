"""GET /internal/remote-auth: what nginx's auth_request calls for every remote
request (RFD 5). Real middleware, real routing, real AuthLib / AssertionLib /
DeviceKeyService / AssertionService; only the repositories are in-memory."""

import base64
import re
from datetime import UTC, datetime, timedelta
from pathlib import Path
from types import SimpleNamespace

import jwt
import pytest
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
from fastapi.testclient import TestClient
from loguru import logger as loguru_logger

import cloud_api
from cloud_api.interface.router import app
from cloud_api.lib.assertion_lib import VALID_SCOPES, AssertionLib
from cloud_api.lib.auth_lib import AuthLib, InvalidTokenError, JWTClaims
from cloud_api.service.assertion_service import AssertionService
from cloud_api.service.device_key_service import DeviceKeyService
from cloud_api.service.remote_auth_service import OWNER_SCOPES, RemoteAuthService
from cloud_api.service_manager import get_remote_auth_service
from tests.ufctl_fakes import FakeDeviceKeyRepo

URL = f"{cloud_api.app_config.api.base_path}/internal/remote-auth"
CLUSTER_HEADER = "X-Underleaf-Cluster"
ASSERTION_HEADER = "X-Underleaf-Assertion"
CONNECTION_HEADER = "X-Underleaf-Connection"

ALICE, BOB = "principal_alice", "principal_bob"


class FakeClusterRepo:
    def __init__(self):
        self.clusters = {
            "cluster_alice": SimpleNamespace(
                id="cluster_alice", principal_account_id=ALICE
            ),
            "cluster_bob": SimpleNamespace(id="cluster_bob", principal_account_id=BOB),
        }

    async def get_cluster(self, cluster_id):
        return self.clusters.get(cluster_id)


class FakeConnRepo:
    def __init__(self):
        self.by_cluster: dict[str, str] = {}
        self.asked: list[str] = []

    async def get_standing_remote_connection(self, cluster_id):
        self.asked.append(cluster_id)
        cid = self.by_cluster.get(cluster_id)
        return SimpleNamespace(id=cid) if cid else None


def _auth_lib() -> AuthLib:
    lib = AuthLib.__new__(AuthLib)  # skip bootstrapping CA files from disk
    lib._private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    lib._public_key = lib._private_key.public_key()
    return lib


@pytest.fixture
def env(tmp_path):
    keys = FakeDeviceKeyRepo()
    conns = FakeConnRepo()
    assertion_lib = AssertionLib(str(tmp_path / "keys"))
    auth_lib = _auth_lib()
    device_keys = DeviceKeyService(keys)
    svc = RemoteAuthService(
        auth_lib=auth_lib,
        device_key_service=device_keys,
        assertion_service=AssertionService(assertion_lib, FakeClusterRepo()),
        conn_repo=conns,
    )
    app.dependency_overrides[get_remote_auth_service] = lambda: svc
    try:
        # No `with`: do not run the app's startup hooks (DB, usage loop).
        yield SimpleNamespace(
            client=TestClient(app),
            auth_lib=auth_lib,
            assertion_lib=assertion_lib,
            device_keys=device_keys,
            keys=keys,
            conns=conns,
        )
    finally:
        app.dependency_overrides.pop(get_remote_auth_service, None)


def _jwt_for(env, account=ALICE, user="user_alice"):
    return env.auth_lib.mint_access_token(user_id=user, principal_account_id=account)


async def _device_key(env, account=ALICE):
    return (await env.device_keys.create_for_account(account, "laptop"))[1]


def _call(env, credential, cluster="cluster_alice", **extra):
    headers = dict(extra)
    if credential is not None:
        headers["Authorization"] = f"Bearer {credential}"
    if cluster is not None:
        headers[CLUSTER_HEADER] = cluster
    return env.client.get(URL, headers=headers)


def _verify(env, token) -> dict:
    """Verify the assertion's signature against the published key set."""
    header = jwt.get_unverified_header(token)
    (jwk,) = [k for k in env.assertion_lib.jwks()["keys"] if k["kid"] == header["kid"]]
    raw = base64.urlsafe_b64decode(jwk["x"] + "=" * (-len(jwk["x"]) % 4))
    return jwt.decode(
        token,
        Ed25519PublicKey.from_public_bytes(raw),
        algorithms=["EdDSA"],
        audience="cluster_alice",
    )


# ------------------------------------------------------------ happy paths


@pytest.mark.asyncio
async def test_device_key_owner_gets_a_signed_assertion(env):
    env.conns.by_cluster["cluster_alice"] = "conn_standing"
    r = _call(env, await _device_key(env))
    assert r.status_code == 200
    assert r.content == b""
    claims = _verify(env, r.headers[ASSERTION_HEADER])
    assert claims["sub"] == ALICE and claims["aud"] == "cluster_alice"
    assert claims["exp"] - claims["iat"] == 60
    assert claims["iss"] == "underleaf-cloud"
    assert r.headers[CONNECTION_HEADER] == "conn_standing"
    assert r.headers["cache-control"] == "no-store"


@pytest.mark.asyncio
async def test_cockpit_jwt_owner_is_accepted_too(env):
    r = _call(env, _jwt_for(env))
    assert r.status_code == 200
    assert _verify(env, r.headers[ASSERTION_HEADER])["sub"] == ALICE


@pytest.mark.asyncio
async def test_owner_gets_every_scope_the_orchestrator_defines(env):
    r = _call(env, await _device_key(env))
    scopes = _verify(env, r.headers[ASSERTION_HEADER])["scopes"]
    assert set(scopes) == {"read", "deploy", "admin"} == set(VALID_SCOPES)
    assert set(OWNER_SCOPES) == set(scopes)


def test_owner_scopes_match_the_go_constants():
    """The scope strings live in shared/assertion and the Orchestrator's
    service package; if either changes this fails instead of silently granting
    a scope the edge ignores (or omitting one it defines)."""
    root = Path(__file__).resolve().parents[4]
    files = [
        root / "shared/assertion/claims.go",
        root / "edge/orchestrator/service/token_service.go",
    ]
    if not all(f.exists() for f in files):
        pytest.skip("Go sources not present")
    for f in files:
        found = set(
            re.findall(
                r'Scope(?:Read|Deploy|Admin)\s+(?:Scope\s+)?=\s+"(\w+)"', f.read_text()
            )
        )
        assert found == set(OWNER_SCOPES), f


@pytest.mark.asyncio
async def test_no_standing_connection_is_an_empty_header_not_a_missing_one(env):
    r = _call(env, await _device_key(env))
    assert r.status_code == 200
    assert CONNECTION_HEADER in r.headers
    assert r.headers[CONNECTION_HEADER] == ""
    assert env.conns.asked == ["cluster_alice"]


@pytest.mark.asyncio
async def test_each_request_gets_a_fresh_assertion(env):
    key = await _device_key(env)
    a = _call(env, key).headers[ASSERTION_HEADER]
    b = _call(env, key).headers[ASSERTION_HEADER]
    assert a != b
    assert _verify(env, a)["jti"] != _verify(env, b)["jti"]


@pytest.mark.asyncio
async def test_authorization_scheme_is_case_insensitive(env):
    key = await _device_key(env)
    r = env.client.get(
        URL, headers={"Authorization": f"bearer {key}", CLUSTER_HEADER: "cluster_alice"}
    )
    assert r.status_code == 200


# ----------------------------------- (a) the X-Subject-Id bypass cannot help


@pytest.mark.asyncio
async def test_x_subject_id_is_refused_with_400_even_with_a_valid_key(env):
    key = await _device_key(env)
    r = _call(env, key, **{"X-Subject-Id": "cluster_alice"})
    assert r.status_code == 400
    assert r.content == b"" and ASSERTION_HEADER not in r.headers


def test_x_subject_id_alone_is_400_and_the_middleware_never_mints(env, monkeypatch):
    """No Authorization, only X-Subject-Id: on any other route the middleware
    would mint a token for the cluster's account here. On this route it must
    not even look the id up."""
    from cloud_api.interface import deps

    def boom(*a, **k):
        raise AssertionError("middleware tried to mint a token on /internal/")

    monkeypatch.setattr(deps, "fetch_data_for_node_or_cluster", boom)
    monkeypatch.setattr(deps, "mint_token", boom)
    for subject in ("cluster_alice", "node_whatever", "garbage", ""):
        r = env.client.get(
            URL, headers={"X-Subject-Id": subject, CLUSTER_HEADER: "cluster_alice"}
        )
        assert r.status_code == 400, subject
        assert ASSERTION_HEADER not in r.headers


def test_x_subject_id_header_name_is_matched_case_insensitively(env):
    for name in ("x-subject-id", "X-SUBJECT-ID", "X-Subject-ID"):
        r = env.client.get(URL, headers={name: "cluster_alice"})
        assert r.status_code == 400, name


@pytest.mark.asyncio
async def test_a_token_minted_for_a_cluster_is_not_a_credential(env):
    """What the X-Subject-Id middleware produces (sub = cluster/node id, azp =
    the owner) is a perfectly signed access token. Even if one reached this
    endpoint some other way, it is not a person."""
    for subject in ("cluster_alice", "node_alice", ""):
        token = env.auth_lib.mint_access_token(
            user_id=subject, principal_account_id=ALICE
        )
        assert _call(env, token).status_code == 401, subject


@pytest.mark.asyncio
async def test_a_minted_bearer_cannot_be_mistaken_for_a_device_key(env):
    """The device-key path takes ufdk_ credentials only, and a JWT is never
    looked up as a key; nothing a client can mint carries the prefix."""
    token = _jwt_for(env)
    assert not token.startswith("ufdk_")
    # a device-key-shaped string is judged as a key, not as a JWT
    assert _call(env, "ufdk_" + token[:43]).status_code == 401
    # a real key's secret does not validate as a JWT either
    key = await _device_key(env)
    with pytest.raises(InvalidTokenError):
        env.auth_lib.validate_token(key)


# ------------------------------------------- (c) credentials that are not ok


@pytest.mark.asyncio
async def test_revoked_unknown_and_malformed_device_keys_are_401(env):
    key = await _device_key(env)
    (stored,) = env.keys.rows.values()
    assert _call(env, key).status_code == 200
    stored.revoked_at = datetime.now(UTC)
    r = _call(env, key)
    assert r.status_code == 401 and ASSERTION_HEADER not in r.headers
    assert _call(env, "ufdk_" + "A" * 43).status_code == 401
    assert _call(env, "ufdk_nope").status_code == 401


@pytest.mark.asyncio
async def test_expired_jwt_is_401(env):
    now = datetime.now(UTC)
    claims = JWTClaims(
        sub="user_alice",
        azp=ALICE,
        iat=int((now - timedelta(hours=3)).timestamp()),
        exp=int((now - timedelta(hours=2)).timestamp()),
        iss=cloud_api.app_config.oauth.issuer,
        aud=cloud_api.app_config.oauth.audience,
    )
    assert _call(env, env.auth_lib.mint_token(claims)).status_code == 401


@pytest.mark.asyncio
async def test_refresh_token_is_401(env):
    refresh = env.auth_lib.mint_refresh_token("user_alice")
    r = _call(env, refresh)
    assert r.status_code == 401 and ASSERTION_HEADER not in r.headers


@pytest.mark.asyncio
async def test_tampered_foreign_and_garbage_tokens_are_401(env):
    good = _jwt_for(env)
    other_lib = _auth_lib()  # a different signing key
    forged = other_lib.mint_access_token(
        user_id="user_alice", principal_account_id=ALICE
    )
    unsigned = jwt.encode(
        {
            "sub": "user_alice",
            "azp": ALICE,
            "exp": 9999999999,
            "iat": 1,
            "iss": cloud_api.app_config.oauth.issuer,
            "aud": cloud_api.app_config.oauth.audience,
        },
        key="",
        algorithm="none",
    )
    for bad in (good[:-4] + "AAAA", forged, unsigned, "x.y.z", "garbage", "Bearer"):
        assert _call(env, bad).status_code == 401, bad


@pytest.mark.asyncio
async def test_an_assertion_is_not_a_credential(env):
    token = env.assertion_lib.mint(
        account_id=ALICE, cluster_id="cluster_alice", scopes=["read"]
    )
    assert _call(env, token).status_code == 401


def test_missing_or_malformed_authorization_is_401(env):
    assert _call(env, None).status_code == 401
    for value in (
        "",
        "Basic dXNlcjpwdw==",
        "Bearer",
        "Bearer  ",
        "ufdk_x",
        "Bearer a b",
    ):
        r = env.client.get(
            URL, headers={"Authorization": value, CLUSTER_HEADER: "cluster_alice"}
        )
        assert r.status_code == 401, value


@pytest.mark.asyncio
async def test_two_authorization_headers_are_ambiguous_and_401(env):
    key = await _device_key(env)
    r = env.client.get(
        URL,
        headers=[
            ("Authorization", f"Bearer {key}"),
            ("Authorization", f"Bearer {key}"),
            (CLUSTER_HEADER, "cluster_alice"),
        ],
    )
    assert r.status_code == 401


@pytest.mark.asyncio
async def test_oversized_credential_is_401(env):
    assert _call(env, "ufdk_" + "A" * 100_000).status_code == 401


def test_failures_do_not_leak_detail(env):
    r = _call(env, "ufdk_" + "A" * 43)
    assert r.status_code == 401
    assert r.content == b""  # no body, no "detail"


# ------------------------------------- (d) foreign cluster == missing cluster


@pytest.mark.asyncio
async def test_foreign_and_nonexistent_clusters_are_one_identical_403(env):
    key = await _device_key(env, ALICE)
    jwt_token = _jwt_for(env, ALICE)
    seen = set()
    for credential in (key, jwt_token):
        for cluster in ("cluster_bob", "cluster_does_not_exist"):
            r = _call(env, credential, cluster=cluster)
            assert r.status_code == 403
            seen.add(
                (
                    r.status_code,
                    r.content,
                    tuple(sorted((k, v) for k, v in r.headers.items() if k != "date")),
                )
            )
            assert ASSERTION_HEADER not in r.headers
            assert CONNECTION_HEADER not in r.headers
    assert len(seen) == 1
    # the standing connection of someone else's cluster is never even looked up
    assert env.conns.asked == []


@pytest.mark.asyncio
async def test_bad_cluster_headers_are_the_same_403(env):
    key = await _device_key(env)
    for cluster in (
        None,
        "",
        "cluster_alice/../x",
        "a b",
        "x" * 500,
        "cluster_alice\n",
    ):
        r = _call(env, key, cluster=cluster)
        assert r.status_code == 403, repr(cluster)
        assert r.content == b""
    r = env.client.get(
        URL,
        headers=[
            ("Authorization", f"Bearer {key}"),
            (CLUSTER_HEADER, "cluster_alice"),
            (CLUSTER_HEADER, "cluster_alice"),
        ],
    )
    assert r.status_code == 403


@pytest.mark.asyncio
async def test_authentication_is_judged_before_the_cluster(env):
    """A bad credential must not learn anything about cluster ids."""
    assert _call(env, "ufdk_" + "A" * 43, cluster="cluster_bob").status_code == 401
    assert _call(env, "ufdk_" + "A" * 43, cluster="cluster_nope").status_code == 401


# ---------------------------------- (e) client-supplied assertion never echoed


@pytest.mark.asyncio
async def test_client_supplied_assertion_is_never_echoed(env):
    key = await _device_key(env)
    evil = "attacker.supplied.assertion"
    ok = _call(env, key, **{ASSERTION_HEADER: evil})
    assert ok.status_code == 200
    assert ok.headers[ASSERTION_HEADER] != evil
    assert _verify(env, ok.headers[ASSERTION_HEADER])["sub"] == ALICE
    for r in (
        _call(env, None, **{ASSERTION_HEADER: evil}),
        _call(env, key, cluster="cluster_bob", **{ASSERTION_HEADER: evil}),
        _call(env, key, **{"X-Subject-Id": "x", ASSERTION_HEADER: evil}),
    ):
        assert r.status_code in (400, 401, 403)
        assert ASSERTION_HEADER not in r.headers
        assert evil not in str(r.headers) and evil not in r.text
    # nor the connection header
    spoofed = _call(env, key, **{CONNECTION_HEADER: "conn_attacker"})
    assert spoofed.headers[CONNECTION_HEADER] == ""


# ---------------------------------------------------- (b) nothing is logged


@pytest.mark.asyncio
async def test_credentials_and_assertions_are_never_logged(env):
    key = await _device_key(env)
    token = _jwt_for(env)
    lines: list[str] = []
    sink = loguru_logger.add(
        lambda m: lines.append(str(m)), level="TRACE", serialize=True
    )
    try:
        env.conns.by_cluster["cluster_alice"] = "conn_x"
        responses = [
            _call(env, key),
            _call(env, token),
            _call(env, key, cluster="cluster_bob"),
            _call(env, "ufdk_" + "B" * 43),
            _call(env, token + "x"),
            _call(env, key, **{"X-Subject-Id": "cluster_alice"}),
        ]
    finally:
        loguru_logger.remove(sink)
    secrets_ = [key, token, key[len("ufdk_") :]] + [
        r.headers[ASSERTION_HEADER] for r in responses if ASSERTION_HEADER in r.headers
    ]
    assert any(ASSERTION_HEADER in r.headers for r in responses)
    blob = "\n".join(lines)
    assert lines, "expected the service to log something"
    for s in secrets_:
        assert s not in blob
    assert "ufdk_BBBB" not in blob


# ------------------------------------------------------------- routing


def test_only_get_is_allowed_and_route_is_under_the_api_base(env):
    assert env.client.post(URL).status_code == 405
    assert env.client.delete(URL).status_code == 405
    routes = {
        (m, r.path) for r in app.routes if hasattr(r, "methods") for m in r.methods
    }
    assert ("GET", URL) in routes
