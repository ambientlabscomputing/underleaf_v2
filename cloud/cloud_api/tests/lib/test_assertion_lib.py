"""AssertionLib: dedicated-key signing, key set publication, rotation.

The verification here uses PyJWT directly (not AssertionLib) so the tests do
not trust the code under test to judge its own output. The Go verifier
(shared/assertion) is checked against this issuer's output by the vector test
at the bottom.
"""

import json
import os
import pathlib
import stat
from datetime import UTC, datetime, timedelta

import jwt
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

from cloud_api.lib.assertion_lib import (
    TOKEN_TYPE,
    AssertionLib,
    InvalidAssertionRequestError,
    _b64,
    thumbprint,
)
from cloud_api.lib.auth_lib import AuthLib, InvalidTokenError

T0 = datetime(2026, 12, 1, tzinfo=UTC)
VECTORS = (
    pathlib.Path(__file__).resolve().parents[4] / "shared" / "assertion" / "testdata"
)


def b64d(s: str) -> bytes:
    import base64

    return base64.urlsafe_b64decode(s + "=" * (-len(s) % 4))


def verify(token: str, jwks: dict, now: datetime, audience: str = "cluster_1") -> dict:
    """Independent reference verifier: pick the key by kid, honour the key's
    stated window, EdDSA only, then the usual claim checks against `now`."""
    header = jwt.get_unverified_header(token)
    assert header["alg"] == "EdDSA" and header["typ"] == TOKEN_TYPE
    matches = [k for k in jwks["keys"] if k["kid"] == header["kid"]]
    if not matches:
        raise jwt.InvalidKeyError("unknown kid")
    k = matches[0]
    if not (k["not_before"] <= now.timestamp() < k["not_after"]):
        raise jwt.InvalidKeyError("key outside validity window")
    pub = Ed25519PublicKey.from_public_bytes(b64d(k["x"]))
    claims = jwt.decode(
        token,
        pub,
        algorithms=["EdDSA"],
        audience=audience,
        options={"verify_exp": False, "verify_iat": False},
    )
    if claims["exp"] <= now.timestamp():
        raise jwt.ExpiredSignatureError("expired")
    return claims


@pytest.fixture
def lib(tmp_path):
    return AssertionLib(str(tmp_path / "keys"), now=T0)


def mint(lib, now=T0, **kw):
    args = {"account_id": "acct_1", "cluster_id": "cluster_1", "scopes": ["read"]}
    args.update(kw)
    return lib.mint(now=now, **args)


# --------------------------------------------------------------- claims


def test_assertion_claims_and_header(lib):
    tok = mint(lib, scopes=["deploy", "read", "read"])
    header = jwt.get_unverified_header(tok)
    assert header == {"alg": "EdDSA", "typ": TOKEN_TYPE, "kid": lib.current_kid(T0)}
    claims = verify(tok, lib.jwks(T0), T0)
    assert claims["sub"] == "acct_1"
    assert claims["aud"] == "cluster_1"
    assert claims["scopes"] == ["deploy", "read"]  # sorted, de-duplicated
    assert claims["iss"] == "underleaf-cloud"
    assert claims["exp"] - claims["iat"] == 60
    assert claims["iat"] == int(T0.timestamp())
    assert len(claims["jti"]) >= 16


def test_jti_is_unique_per_assertion(lib):
    assert (
        len(
            {
                jwt.decode(mint(lib), options={"verify_signature": False})["jti"]
                for _ in range(50)
            }
        )
        == 50
    )


@pytest.mark.parametrize(
    "kw",
    [
        {"account_id": ""},
        {"cluster_id": ""},
        {"scopes": []},
        {"scopes": ["root"]},
        {"scopes": ["read", "superuser"]},
        {"ttl_seconds": 0},
        {"ttl_seconds": -5},
        {"ttl_seconds": 3600},
    ],
)
def test_mint_rejects_invalid_requests(lib, kw):
    with pytest.raises(InvalidAssertionRequestError):
        mint(lib, **kw)


@pytest.mark.parametrize(
    "kw",
    [
        {"ttl_seconds": 0},
        {"ttl_seconds": 301},
        {"rotate_after_days": 0},
        {"overlap_hours": 0},
    ],
)
def test_constructor_rejects_bad_config(tmp_path, kw):
    with pytest.raises(ValueError):
        AssertionLib(str(tmp_path / "k"), **kw)


# --------------------------------------- distinct from the CA / JWT key


@pytest.fixture
def auth_lib(ca_files):
    """An AuthLib holding the real CA/JWT keypair shape (RSA, from conftest)."""
    _cert_path, key_path = ca_files
    jwt_path = str(pathlib.Path(key_path).with_name("jwt_private_key.pem"))
    AuthLib.bootstrap_jwt_key(jwt_path, key_path)
    a = AuthLib.__new__(AuthLib)
    a._load_keys(jwt_path, key_path)
    return a


def test_assertion_key_is_distinct_from_ca_key(lib, auth_lib, ca_files, tmp_path):
    ca_pub_der = auth_lib._public_key.public_bytes(
        serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo
    )
    ca_priv_pem = pathlib.Path(ca_files[1]).read_bytes()
    jwks = lib.jwks(T0)
    assert jwks["keys"], "a key set must be published"
    for k in jwks["keys"]:
        raw = b64d(k["x"])
        # different algorithm and different material
        assert k["kty"] == "OKP" and auth_lib._public_key.key_size  # RSA
        assert raw not in ca_pub_der
        assert k["kid"] != thumbprint_of_rsa(auth_lib)
    # the key files on disk contain neither CA material nor vice versa
    for pem in (tmp_path / "keys").glob("*.pem"):
        assert pem.read_bytes() != ca_priv_pem
        key = serialization.load_pem_private_key(pem.read_bytes(), password=None)
        assert key.__class__.__name__ == "Ed25519PrivateKey"


def thumbprint_of_rsa(auth_lib) -> str:
    import hashlib

    n = auth_lib._public_key.public_numbers()
    nb = n.n.to_bytes((n.n.bit_length() + 7) // 8, "big")
    eb = n.e.to_bytes((n.e.bit_length() + 7) // 8, "big")
    canon = f'{{"e":"{_b64(eb)}","kty":"RSA","n":"{_b64(nb)}"}}'
    return _b64(hashlib.sha256(canon.encode()).digest())


def test_ca_signed_token_does_not_verify_as_assertion(lib, auth_lib):
    from cloud_api.lib.auth_lib import JWTClaims

    now = int(datetime.now(UTC).timestamp())
    ca_token = auth_lib.mint_token(
        JWTClaims(sub="acct_1", iat=now, exp=now + 60, iss="underleaf", aud="cluster_1")
    )
    jwks = lib.jwks()
    assert jwt.get_unverified_header(ca_token)["alg"] == "RS256"
    # No kid -> not selectable; even if an attacker adds the assertion kid and
    # asserts typ, the algorithm/typ checks and the signature all fail.
    with pytest.raises((AssertionError, KeyError, jwt.PyJWTError)):
        verify(ca_token, jwks, datetime.now(UTC))
    forged = jwt.encode(
        {"sub": "acct_1", "aud": "cluster_1", "exp": now + 60},
        auth_lib._private_key,
        algorithm="RS256",
        headers={"kid": lib.current_kid(), "typ": TOKEN_TYPE},
    )
    with pytest.raises((AssertionError, jwt.PyJWTError)):
        verify(forged, jwks, datetime.now(UTC))


def test_assertion_does_not_validate_as_oauth_token(lib, auth_lib, monkeypatch):
    import cloud_api
    from cloud_api import AppConfig

    monkeypatch.setattr(cloud_api.config, "app_config", AppConfig())
    monkeypatch.setattr("cloud_api.lib.auth_lib.app_config", AppConfig())
    tok = lib.mint(account_id="acct_1", cluster_id="cluster_1", scopes=["admin"])
    with pytest.raises(InvalidTokenError):
        auth_lib.validate_token(tok)


def test_default_config_keeps_assertion_keys_out_of_the_ca_paths():
    from cloud_api.config import AppConfig

    cfg = AppConfig()
    ca_dir = pathlib.Path(cfg.oauth.private_key_path).parent
    keys_dir = pathlib.Path(cfg.assertion.keys_dir)
    assert keys_dir != ca_dir
    assert str(keys_dir) not in (
        cfg.oauth.private_key_path,
        cfg.oauth.root_ca_cert_path,
        cfg.oauth.jwt_private_key_path,
    )


# ------------------------------------------------------- key set / storage


def test_key_set_has_kids_bound_to_key_material(lib):
    (k,) = lib.jwks(T0)["keys"]
    assert k["kid"] == thumbprint(Ed25519PublicKey.from_public_bytes(b64d(k["x"])))
    assert (k["kty"], k["crv"], k["alg"], k["use"]) == (
        "OKP",
        "Ed25519",
        "EdDSA",
        "sig",
    )
    assert k["not_before"] == int(T0.timestamp())
    assert k["not_after"] == int((T0 + timedelta(days=90, hours=168)).timestamp())
    json.dumps(lib.jwks(T0))  # serializable
    assert "d" not in k and "private" not in json.dumps(k)


def test_private_key_files_are_owner_only_and_pem_only(lib, tmp_path):
    d = tmp_path / "keys"
    assert stat.S_IMODE(d.stat().st_mode) == 0o700
    pems = list(d.glob("*.pem"))
    assert len(pems) == 1
    assert stat.S_IMODE(pems[0].stat().st_mode) == 0o600
    # Only the .pem carries private material (the repo git-ignores **/*.pem).
    for f in d.glob("*.json"):
        assert "PRIVATE" not in f.read_text()
        assert set(json.loads(f.read_text())) == {"kid", "created_at", "not_after"}
    assert not list(d.glob("*.tmp"))


def test_keys_persist_across_restart(tmp_path):
    a = AssertionLib(str(tmp_path / "k"), now=T0)
    b = AssertionLib(str(tmp_path / "k"), now=T0 + timedelta(minutes=5))
    assert a.current_kid(T0) == b.current_kid(T0)
    assert verify(mint(a), b.jwks(T0), T0)["sub"] == "acct_1"
    assert len(b.jwks(T0)["keys"]) == 1


def test_tampered_key_files_are_refused(tmp_path):
    AssertionLib(str(tmp_path / "k"), now=T0)
    pem = next((tmp_path / "k").glob("*.pem"))
    pem.rename(pem.with_name("renamed.pem"))
    (pem.with_suffix(".json")).rename(pem.with_name("renamed.json"))
    with pytest.raises(RuntimeError):
        AssertionLib(str(tmp_path / "k"), now=T0)


# ----------------------------------------------------------------- rotation


def test_rotation_with_overlap(tmp_path):
    lib = AssertionLib(
        str(tmp_path / "k"), now=T0, rotate_after_days=90, overlap_hours=24
    )
    old_kid = lib.current_kid(T0)
    old_tok = mint(lib, now=T0)

    t_rot = T0 + timedelta(days=30)
    new_key = lib.rotate(now=t_rot)
    assert new_key.kid != old_kid

    jwks = lib.jwks(t_rot)
    assert [k["kid"] for k in jwks["keys"]] == [old_kid, new_key.kid]
    old_entry = jwks["keys"][0]
    assert old_entry["not_after"] == int((t_rot + timedelta(hours=24)).timestamp())

    # new tokens use the new key; the old token still verifies during the overlap
    new_tok = mint(lib, now=t_rot)
    assert jwt.get_unverified_header(new_tok)["kid"] == new_key.kid
    assert verify(new_tok, jwks, t_rot)
    assert verify(old_tok, jwks, T0 + timedelta(seconds=30))  # signed before, in flight
    old_late = jwt.encode(
        {"sub": "a", "aud": "cluster_1", "iat": 1, "exp": 4102444800},
        lib._keys[0].private_key,
        algorithm="EdDSA",
        headers={"kid": old_kid, "typ": TOKEN_TYPE},
    )
    assert verify(old_late, jwks, t_rot + timedelta(hours=23))

    # after the overlap the old key is gone from the set and fails
    t_after = t_rot + timedelta(hours=24)
    jwks_after = lib.jwks(t_after)
    assert [k["kid"] for k in jwks_after["keys"]] == [new_key.kid]
    with pytest.raises(jwt.InvalidKeyError):
        verify(old_late, jwks_after, t_after)
    assert verify(mint(lib, now=t_after), jwks_after, t_after)
    # and its files are deleted
    assert not (tmp_path / "k" / f"{old_kid}.pem").exists()
    assert not (tmp_path / "k" / f"{old_kid}.json").exists()


def test_old_key_stays_published_if_rotation_is_early_and_cap_only_shortens(tmp_path):
    lib = AssertionLib(
        str(tmp_path / "k"), now=T0, rotate_after_days=10, overlap_hours=24
    )
    first = lib.current_kid(T0)
    # lazy rotation: first use is 20 days in -> old key's original not_after
    # (T0+10d+24h) has passed, it must not be resurrected by the overlap cap.
    late = T0 + timedelta(days=20)
    new_kid = lib.current_kid(late)
    assert new_kid != first
    assert [k["kid"] for k in lib.jwks(late)["keys"]] == [new_kid]


def test_automatic_rotation_after_rotate_after_days(tmp_path):
    lib = AssertionLib(
        str(tmp_path / "k"), now=T0, rotate_after_days=90, overlap_hours=24
    )
    first = lib.current_kid(T0)
    assert lib.current_kid(T0 + timedelta(days=89)) == first
    second = lib.current_kid(T0 + timedelta(days=90))
    assert second != first
    keys = lib.jwks(T0 + timedelta(days=90))["keys"]
    assert [k["kid"] for k in keys] == [first, second]
    # minting after the boundary uses the new key
    assert (
        jwt.get_unverified_header(mint(lib, now=T0 + timedelta(days=91)))["kid"]
        == second
    )


def test_compromised_rotation_expires_old_keys_immediately(tmp_path):
    lib = AssertionLib(str(tmp_path / "k"), now=T0)
    old = lib.current_kid(T0)
    tok = mint(lib, now=T0)
    new = lib.rotate(now=T0 + timedelta(minutes=1), compromised=True)
    jwks = lib.jwks(T0 + timedelta(minutes=1))
    assert [k["kid"] for k in jwks["keys"]] == [new.kid]
    with pytest.raises(jwt.InvalidKeyError):
        verify(tok, jwks, T0 + timedelta(minutes=1))
    assert old != new.kid


def test_rotation_by_another_process_is_picked_up(tmp_path):
    a = AssertionLib(str(tmp_path / "k"), now=T0)  # the running cloud_api
    first = a.current_kid(T0)
    b = AssertionLib(str(tmp_path / "k"), now=T0)  # the rotate script
    new = b.rotate(now=T0 + timedelta(minutes=2))
    now = T0 + timedelta(minutes=3)
    os.utime(tmp_path / "k")  # guard against coarse directory mtime granularity
    assert a.current_kid(now) == new.kid != first
    assert {k["kid"] for k in a.jwks(now)["keys"]} == {first, new.kid}


# ------------------------------------------------- Go interop (both ways)


def test_go_verifier_accepts_what_this_issuer_signs(tmp_path):
    """Writes (UPDATE_ASSERTION_VECTORS=1) or checks the vector that
    shared/assertion's TestInteropWithPythonIssuer verifies with the Go verifier
    at the fixed time recorded in it."""
    path = VECTORS / "python_issued.json"
    if os.environ.get("UPDATE_ASSERTION_VECTORS") == "1":
        lib = AssertionLib(str(tmp_path / "k"), now=T0)
        token = lib.mint(
            account_id="acct_interop",
            cluster_id="cluster_interop",
            scopes=["read", "deploy"],
            now=T0,
        )
        path.write_text(
            json.dumps(
                {
                    "comment": "Issued by cloud_api AssertionLib; verified by Go shared/assertion. "
                    "Regenerate: UPDATE_ASSERTION_VECTORS=1 pytest tests/lib/test_assertion_lib.py",
                    "now": int(T0.timestamp()),
                    "audience": "cluster_interop",
                    "subject": "acct_interop",
                    "scopes": ["deploy", "read"],
                    "jwks": lib.jwks(T0),
                    "token": token,
                },
                indent=2,
            )
            + "\n"
        )
    v = json.loads(path.read_text())
    now = datetime.fromtimestamp(v["now"], UTC)
    claims = verify(v["token"], v["jwks"], now, audience=v["audience"])
    assert claims["sub"] == v["subject"] and claims["scopes"] == v["scopes"]


def test_this_issuers_verifier_logic_accepts_what_go_signs():
    v = json.loads((VECTORS / "go_issued.json").read_text())
    now = datetime.fromtimestamp(v["now"], UTC)
    claims = verify(v["token"], v["jwks"], now, audience=v["audience"])
    assert claims["sub"] == v["subject"]
