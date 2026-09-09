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
