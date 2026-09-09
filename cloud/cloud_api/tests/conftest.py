"""Shared fixtures for cloud_api's unit tests."""

from datetime import UTC, datetime, timedelta

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID

import cloud_api
from cloud_api import load_config
from cloud_api.lib.cert_lib import CertLib

# cloud_api.service_manager eagerly constructs a singleton AuthLib as soon as
# cloud_api.interface/service_manager is imported (e.g. by tests/interface/
# test_deps.py), which requires cloud_api.app_config to already be set --
# normally done by main.py before it imports anything else. Replicate that
# here so it runs before pytest collects any sibling test module. CertLib
# itself has no such dependency, so importing it above this line is fine.
cloud_api.app_config = load_config()


def _generate_self_signed_ca():
    """Build a throwaway CA keypair, matching AuthLib's real bootstrap shape
    closely enough for CertLib to load and sign with (2048-bit RSA, a
    self-signed cert with CommonName as its only identifying attribute).
    """
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "Test Root CA")])
    now = datetime.now(UTC)
    cert = (
        x509.CertificateBuilder()
        .subject_name(name)
        .issuer_name(name)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - timedelta(days=1))
        .not_valid_after(now + timedelta(days=3650))
        .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
        .sign(key, hashes.SHA256())
    )
    key_pem = key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.TraditionalOpenSSL,
        encryption_algorithm=serialization.NoEncryption(),
    )
    cert_pem = cert.public_bytes(serialization.Encoding.PEM)
    return cert_pem, key_pem


@pytest.fixture
def ca_files(tmp_path):
    """Write a throwaway self-signed CA to disk; return (cert_path, key_path)."""
    cert_pem, key_pem = _generate_self_signed_ca()
    cert_path = tmp_path / "root_ca_cert.pem"
    key_path = tmp_path / "private_key.pem"
    cert_path.write_bytes(cert_pem)
    key_path.write_bytes(key_pem)
    return str(cert_path), str(key_path)


@pytest.fixture
def cert_lib(ca_files) -> CertLib:
    cert_path, key_path = ca_files
    return CertLib(cert_path, key_path)


def generate_csr_pem(common_name: str = "csr-subject") -> str:
    """A CSR's own subject is irrelevant to CertLib.sign_csr (it only reads
    the public key off the CSR and sets CN from the subject_id argument), so
    any valid CSR works here.
    """
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    csr = (
        x509.CertificateSigningRequestBuilder()
        .subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, common_name)]))
        .sign(key, hashes.SHA256())
    )
    return csr.public_bytes(serialization.Encoding.PEM).decode()
