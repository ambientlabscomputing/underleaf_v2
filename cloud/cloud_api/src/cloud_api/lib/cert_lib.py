"""
cert_lib.py — Utilities for signing device CSRs with the cloud-api root CA.

The root CA private key is the same RSA key used for JWT signing.  Device
certificates are issued with extendedKeyUsage=clientAuth so they can be used
for mutual TLS authentication by registered cluster orchestrators.
"""

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric.rsa import RSAPrivateKey
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID
from datetime import datetime, timedelta, timezone


class CertLib:
    def __init__(self, cert_path: str, key_path: str) -> None:
        with open(key_path, "rb") as f:
            key = serialization.load_pem_private_key(f.read(), password=None)
            assert isinstance(key, RSAPrivateKey), "Expected RSA private key"
            self._ca_key: RSAPrivateKey = key

        with open(cert_path, "rb") as f:
            raw = f.read()
            self._ca_cert = x509.load_pem_x509_certificate(raw)
            self._ca_cert_pem: str = raw.decode()

    def sign_csr(
        self,
        csr_pem: str,
        subject_id: str,
        validity_days: int = 730,
    ) -> tuple[str, str]:
        """Sign a PEM-encoded CSR and return (certificate_pem, ca_chain_pem).

        The issued certificate has:
        - CN = subject_id
        - extendedKeyUsage = clientAuth (critical)
        - subjectAltName = DNS:subject_id
        - validity = validity_days from now
        """
        csr = x509.load_pem_x509_csr(csr_pem.encode())
        now = datetime.now(timezone.utc)

        cert = (
            x509.CertificateBuilder()
            .subject_name(
                x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, subject_id)])
            )
            .issuer_name(self._ca_cert.subject)
            .public_key(csr.public_key())
            .serial_number(x509.random_serial_number())
            .not_valid_before(now)
            .not_valid_after(now + timedelta(days=validity_days))
            .add_extension(
                x509.ExtendedKeyUsage([ExtendedKeyUsageOID.CLIENT_AUTH]),
                critical=True,
            )
            .add_extension(
                x509.SubjectAlternativeName([x509.DNSName(subject_id)]),
                critical=False,
            )
            .sign(self._ca_key, hashes.SHA256())
        )

        cert_pem = cert.public_bytes(serialization.Encoding.PEM).decode()
        return cert_pem, self._ca_cert_pem
