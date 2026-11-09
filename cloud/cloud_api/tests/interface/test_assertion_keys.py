"""GET /assertion-keys publishes the assertion public key set."""

from datetime import UTC, datetime, timedelta

import jwt
import pytest
from fastapi.testclient import TestClient

from cloud_api.interface.router import app
from cloud_api.lib.assertion_lib import AssertionLib
from cloud_api.service_manager import get_assertion_lib

URL = "/api/v2/cloud/assertion-keys"


@pytest.fixture
def lib(tmp_path):
    return AssertionLib(str(tmp_path / "keys"))


@pytest.fixture
def client(lib):
    app.dependency_overrides[get_assertion_lib] = lambda: lib
    try:
        # No `with`: do not run the app's startup hooks (DB, usage loop).
        yield TestClient(app)
    finally:
        app.dependency_overrides.pop(get_assertion_lib, None)


def test_key_set_is_served_unauthenticated_with_kids(client, lib):
    r = client.get(URL)  # no Authorization, no client cert header
    assert r.status_code == 200
    assert r.headers["cache-control"] == "public, max-age=300"
    body = r.json()
    (key,) = body["keys"]
    assert key["kid"] == lib.current_kid()
    assert key["kty"] == "OKP" and key["crv"] == "Ed25519" and key["alg"] == "EdDSA"
    assert key["not_after"] > key["not_before"]
    # public material only
    assert set(key) == {
        "kty",
        "crv",
        "kid",
        "alg",
        "use",
        "x",
        "not_before",
        "not_after",
    }


def test_served_key_set_verifies_a_minted_assertion(client, lib):
    token = lib.mint(account_id="a", cluster_id="c", scopes=["read"])
    keys = client.get(URL).json()["keys"]
    assert jwt.get_unverified_header(token)["kid"] in {k["kid"] for k in keys}


def test_rotation_is_visible_in_the_published_set(client, lib):
    old = lib.current_kid()
    new = lib.rotate(now=datetime.now(UTC)).kid
    kids = [k["kid"] for k in client.get(URL).json()["keys"]]
    assert kids == [old, new]  # old key still published during the overlap
    # ...and once it expires it is gone
    lib.jwks(datetime.now(UTC) + timedelta(days=30))
    assert [k["kid"] for k in client.get(URL).json()["keys"]] == [new]


def test_no_assertion_minting_endpoint_is_exposed():
    """No public route mints an assertion: the key set is the only route with
    "assertion" in its path, and minting lives behind /internal/remote-auth."""
    routes = {
        (m, r.path) for r in app.routes if hasattr(r, "methods") for m in r.methods
    }
    assertion_routes = {x for x in routes if "assertion" in x[1]}
    assert assertion_routes == {("GET", URL)}
