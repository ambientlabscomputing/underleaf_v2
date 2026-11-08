"""The OAuth JWT key and the cluster CA key are two different keys (RFD 5)."""

import os
import pathlib
import stat

import jwt
import pytest
from cryptography import x509
from cryptography.hazmat.primitives import serialization

from cloud_api.lib.auth_lib import AuthLib, JWTClaims


def _jwt_path(ca_files):
    return os.path.join(os.path.dirname(ca_files[1]), "jwt_private_key.pem")


def test_bootstrap_creates_a_separate_owner_only_key(ca_files):
    jwt_path = _jwt_path(ca_files)
    assert AuthLib.bootstrap_jwt_key(jwt_path, ca_files[1]) is True
    assert stat.S_IMODE(os.stat(jwt_path).st_mode) == 0o600
    with open(jwt_path, "rb") as f:
        jwt_key = serialization.load_pem_private_key(f.read(), password=None)
    with open(ca_files[1], "rb") as f:
        ca_key = serialization.load_pem_private_key(f.read(), password=None)
    assert jwt_key.public_key().public_numbers() != ca_key.public_key().public_numbers()


def test_bootstrap_does_not_replace_an_existing_key(ca_files):
    jwt_path = _jwt_path(ca_files)
    AuthLib.bootstrap_jwt_key(jwt_path, ca_files[1])
    before = pathlib.Path(jwt_path).read_bytes()
    assert AuthLib.bootstrap_jwt_key(jwt_path, ca_files[1]) is False
    assert pathlib.Path(jwt_path).read_bytes() == before


def test_jwt_key_path_may_not_be_the_ca_key_path(ca_files):
    with pytest.raises(RuntimeError, match="may not be the cluster CA key"):
        AuthLib.bootstrap_jwt_key(ca_files[1], ca_files[1])


def test_loading_refuses_a_jwt_key_that_is_the_ca_key(ca_files):
    """An operator who points both settings at one file (the pre-split layout)
    gets a startup error, not a silently shared key."""
    a = AuthLib.__new__(AuthLib)
    with pytest.raises(RuntimeError, match="same key as the cluster CA key"):
        a._load_keys(ca_files[1], ca_files[1])


def test_token_signed_by_the_old_shared_key_no_longer_validates(ca_files, monkeypatch):
    """The migration: tokens minted with the CA key (old behaviour) are rejected
    once the JWT key is separate, which is why users sign in once more."""
    import cloud_api
    from cloud_api import AppConfig

    monkeypatch.setattr(cloud_api.config, "app_config", AppConfig())
    monkeypatch.setattr("cloud_api.lib.auth_lib.app_config", AppConfig())
    jwt_path = _jwt_path(ca_files)
    AuthLib.bootstrap_jwt_key(jwt_path, ca_files[1])
    a = AuthLib.__new__(AuthLib)
    a._load_keys(jwt_path, ca_files[1])
    with open(ca_files[1], "rb") as f:
        ca_key = serialization.load_pem_private_key(f.read(), password=None)
    claims = JWTClaims(
        sub="u1", iat=1, exp=4_000_000_000, iss="underleaf", aud="underleaf"
    )
    old_token = jwt.encode(claims.model_dump(), ca_key, algorithm="RS256")
    from cloud_api.lib.auth_lib import InvalidTokenError

    with pytest.raises(InvalidTokenError):
        a.validate_token(old_token)
    # and a token from the new key does validate
    assert a.validate_token(a.mint_token(claims)).sub == "u1"
    # the new public key is not the CA certificate's key
    cert = x509.load_pem_x509_certificate(pathlib.Path(ca_files[0]).read_bytes())
    assert a._public_key.public_numbers() != cert.public_key().public_numbers()
