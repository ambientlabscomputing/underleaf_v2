from datetime import UTC, datetime, timedelta

from cryptography import x509
from cryptography.hazmat.primitives.asymmetric import padding
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

from tests.conftest import generate_csr_pem


def test_sign_csr_sets_expected_fields(cert_lib):
    subject_id = "cluster_abc123"
    csr_pem = generate_csr_pem()

    cert_pem, ca_chain_pem = cert_lib.sign_csr(csr_pem, subject_id)

    cert = x509.load_pem_x509_certificate(cert_pem.encode())

    cn = cert.subject.get_attributes_for_oid(NameOID.COMMON_NAME)[0].value
    assert cn == subject_id

    eku = cert.extensions.get_extension_for_class(x509.ExtendedKeyUsage)
    assert eku.critical is True
    assert ExtendedKeyUsageOID.CLIENT_AUTH in eku.value

    san = cert.extensions.get_extension_for_class(x509.SubjectAlternativeName)
    assert san.value.get_values_for_type(x509.DNSName) == [subject_id]

    assert ca_chain_pem == cert_lib._ca_cert_pem


def test_sign_csr_default_validity_is_730_days(cert_lib):
    csr_pem = generate_csr_pem()
    cert_pem, _ = cert_lib.sign_csr(csr_pem, "cluster_abc123")
    cert = x509.load_pem_x509_certificate(cert_pem.encode())

    delta = cert.not_valid_after_utc - cert.not_valid_before_utc
    assert timedelta(days=729) < delta < timedelta(days=731)

    assert cert.not_valid_after_utc > datetime.now(UTC) + timedelta(days=700)


def test_sign_csr_respects_custom_validity(cert_lib):
    csr_pem = generate_csr_pem()
    cert_pem, _ = cert_lib.sign_csr(csr_pem, "cluster_abc123", validity_days=30)
    cert = x509.load_pem_x509_certificate(cert_pem.encode())

    delta = cert.not_valid_after_utc - cert.not_valid_before_utc
    assert timedelta(days=29) < delta < timedelta(days=31)


def test_signed_cert_verifies_against_the_ca(cert_lib):
    csr_pem = generate_csr_pem()
    cert_pem, _ = cert_lib.sign_csr(csr_pem, "cluster_abc123")
    cert = x509.load_pem_x509_certificate(cert_pem.encode())

    ca_public_key = cert_lib._ca_cert.public_key()
    # Raises InvalidSignature if the cert wasn't actually signed by this CA key.
    ca_public_key.verify(
        cert.signature,
        cert.tbs_certificate_bytes,
        padding.PKCS1v15(),
        cert.signature_hash_algorithm,
    )


def test_issue_server_cert_is_server_auth_with_our_sans(cert_lib):
    cert_pem, ca_pem = cert_lib.issue_server_cert(
        generate_csr_pem("whatever-the-vm-asked-for"),
        common_name="worker_1",
        dns_names=["tunnel.example.com"],
        ip_addresses=["203.0.113.7"],
    )
    cert = x509.load_pem_x509_certificate(cert_pem.encode())

    assert (
        cert.subject.get_attributes_for_oid(NameOID.COMMON_NAME)[0].value == "worker_1"
    )
    eku = cert.extensions.get_extension_for_class(x509.ExtendedKeyUsage)
    assert list(eku.value) == [ExtendedKeyUsageOID.SERVER_AUTH]
    san = cert.extensions.get_extension_for_class(x509.SubjectAlternativeName).value
    assert san.get_values_for_type(x509.DNSName) == ["tunnel.example.com"]
    assert [str(i) for i in san.get_values_for_type(x509.IPAddress)] == ["203.0.113.7"]
    assert ca_pem == cert_lib.ca_pem


def test_issue_server_cert_requires_a_san(cert_lib):
    import pytest

    with pytest.raises(ValueError):
        cert_lib.issue_server_cert(generate_csr_pem(), common_name="w")


def test_issue_client_identity_is_client_auth_and_matches_its_key(cert_lib):
    from cryptography.hazmat.primitives import serialization

    cert_pem, key_pem = cert_lib.issue_client_identity("cloud_api")
    cert = x509.load_pem_x509_certificate(cert_pem.encode())
    key = serialization.load_pem_private_key(key_pem.encode(), password=None)

    assert (
        cert.subject.get_attributes_for_oid(NameOID.COMMON_NAME)[0].value == "cloud_api"
    )
    eku = cert.extensions.get_extension_for_class(x509.ExtendedKeyUsage)
    assert ExtendedKeyUsageOID.CLIENT_AUTH in eku.value
    assert cert.public_key().public_numbers() == key.public_key().public_numbers()
