"""The server certificate nginx presents is CA-issued, name-bearing, and the
directory nginx mounts never holds a CA or JWT key."""

import os
import pathlib
import stat
from datetime import UTC, datetime, timedelta

import pytest
from cryptography import x509
from cryptography.x509.oid import ExtendedKeyUsageOID

from cloud_api.config import TLSConfig
from cloud_api.lib.tls_lib import (
    CA_CERT_FILE,
    SERVER_CERT_FILE,
    SERVER_KEY_FILE,
    ensure_server_cert,
)


@pytest.fixture
def cfg(tmp_path):
    return TLSConfig(
        dir=str(tmp_path / "tls"),
        server_names=["localhost", "cloud.example.test", "*.gw.example.test"],
        server_ips=["127.0.0.1"],
    )


def _load(cfg):
    with open(os.path.join(cfg.dir, SERVER_CERT_FILE), "rb") as f:
        return x509.load_pem_x509_certificate(f.read())


def test_issues_a_ca_signed_leaf_with_names_and_server_auth_only(cert_lib, cfg):
    assert ensure_server_cert(cert_lib, cfg) is True
    leaf = _load(cfg)
    leaf.verify_directly_issued_by(cert_lib.ca_cert)
    san = leaf.extensions.get_extension_for_class(x509.SubjectAlternativeName).value
    assert set(san.get_values_for_type(x509.DNSName)) == set(cfg.server_names)
    assert [str(i) for i in san.get_values_for_type(x509.IPAddress)] == ["127.0.0.1"]
    eku = leaf.extensions.get_extension_for_class(x509.ExtendedKeyUsage).value
    assert list(eku) == [ExtendedKeyUsageOID.SERVER_AUTH]  # never clientAuth
    bc = leaf.extensions.get_extension_for_class(x509.BasicConstraints)
    assert bc.value.ca is False and bc.critical


def test_directory_has_only_the_three_files_and_key_is_private(cert_lib, cfg, ca_files):
    ensure_server_cert(cert_lib, cfg)
    assert sorted(os.listdir(cfg.dir)) == sorted(
        [CA_CERT_FILE, SERVER_CERT_FILE, SERVER_KEY_FILE]
    )
    mode = stat.S_IMODE(os.stat(os.path.join(cfg.dir, SERVER_KEY_FILE)).st_mode)
    assert mode == 0o600
    # The server key is its own key: nothing in the directory is the CA key.
    ca_key = pathlib.Path(ca_files[1]).read_bytes()
    for name in os.listdir(cfg.dir):
        assert ca_key not in pathlib.Path(os.path.join(cfg.dir, name)).read_bytes()
    assert (
        b"PRIVATE KEY"
        in pathlib.Path(os.path.join(cfg.dir, SERVER_KEY_FILE)).read_bytes()
    )


def test_unchanged_config_does_not_reissue(cert_lib, cfg):
    ensure_server_cert(cert_lib, cfg)
    serial = _load(cfg).serial_number
    assert ensure_server_cert(cert_lib, cfg) is False
    assert _load(cfg).serial_number == serial


def test_changed_names_reissue(cert_lib, cfg):
    ensure_server_cert(cert_lib, cfg)
    cfg.server_names = cfg.server_names + ["new.example.test"]
    assert ensure_server_cert(cert_lib, cfg) is True
    san = _load(cfg).extensions.get_extension_for_class(x509.SubjectAlternativeName)
    assert "new.example.test" in san.value.get_values_for_type(x509.DNSName)


def test_near_expiry_reissues(cert_lib, cfg):
    ensure_server_cert(cert_lib, cfg)
    later = datetime.now(UTC) + timedelta(days=cfg.validity_days - 5)
    assert ensure_server_cert(cert_lib, cfg, now=later) is True


def test_a_different_ca_forces_reissue(cert_lib, cfg, tmp_path_factory):
    """If the CA is replaced, the old leaf must not be kept: it no longer
    chains to the certificate clusters pin."""
    from cloud_api.lib.cert_lib import CertLib
    from tests.conftest import _generate_self_signed_ca

    ensure_server_cert(cert_lib, cfg)
    d = tmp_path_factory.mktemp("otherca")
    cert_pem, key_pem = _generate_self_signed_ca()
    (d / "c.pem").write_bytes(cert_pem)
    (d / "k.pem").write_bytes(key_pem)
    other = CertLib(str(d / "c.pem"), str(d / "k.pem"))
    assert ensure_server_cert(other, cfg) is True
    _load(cfg).verify_directly_issued_by(other.ca_cert)


def test_mismatched_key_and_cert_reissues(cert_lib, cfg):
    ensure_server_cert(cert_lib, cfg)
    other_key = cert_lib.issue_server_cert(["x.test"])[1]
    with open(os.path.join(cfg.dir, SERVER_KEY_FILE), "w") as f:
        f.write(other_key)
    assert ensure_server_cert(cert_lib, cfg) is True


def test_a_server_cert_needs_a_name(cert_lib):
    with pytest.raises(ValueError):
        cert_lib.issue_server_cert([], [])
