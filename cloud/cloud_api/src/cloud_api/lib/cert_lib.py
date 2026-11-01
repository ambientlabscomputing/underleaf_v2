"""
cert_lib.py — Utilities for signing device CSRs with the cloud-api root CA.

The root CA private key is the same RSA key used for JWT signing.  Device
certificates are issued with extendedKeyUsage=clientAuth so they can be used
for mutual TLS authentication by registered cluster orchestrators.
"""

import ipaddress
from datetime import UTC, datetime, timedelta

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives.asymmetric.rsa import RSAPrivateKey
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID


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
        now = datetime.now(UTC)

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

    def issue_server_cert(
        self,
        csr_pem: str,
        common_name: str,
        dns_names: list[str] | None = None,
        ip_addresses: list[str] | None = None,
        validity_days: int = 90,
    ) -> tuple[str, str]:
        """Sign a CSR as a TLS *server* certificate (serverAuth) for a worker VM.

        The public key comes from the CSR so the private key never leaves the
        VM. SANs are chosen by us, not taken from the CSR, so a VM can't ask
        for a name it doesn't own. Returns (certificate_pem, ca_pem).
        """
        csr = x509.load_pem_x509_csr(csr_pem.encode())
        if not csr.is_signature_valid:
            raise ValueError("CSR signature is invalid")
        sans: list[x509.GeneralName] = [x509.DNSName(n) for n in dns_names or []]
        sans += [x509.IPAddress(ipaddress.ip_address(a)) for a in ip_addresses or []]
        if not sans:
            raise ValueError("a server certificate needs at least one SAN")
        now = datetime.now(UTC)
        cert = (
            x509.CertificateBuilder()
            .subject_name(
                x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, common_name)])
            )
            .issuer_name(self._ca_cert.subject)
            .public_key(csr.public_key())
            .serial_number(x509.random_serial_number())
            .not_valid_before(now - timedelta(minutes=5))
            .not_valid_after(now + timedelta(days=validity_days))
            .add_extension(
                x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH]), critical=True
            )
            .add_extension(x509.SubjectAlternativeName(sans), critical=False)
            .sign(self._ca_key, hashes.SHA256())
        )
        return cert.public_bytes(serialization.Encoding.PEM).decode(), self._ca_cert_pem

    def issue_client_identity(
        self, subject_id: str, validity_days: int = 365
    ) -> tuple[str, str]:
        """Mint a keypair + clientAuth certificate for this process itself
        (e.g. the API authenticating to a worker over mTLS). Returns
        (certificate_pem, private_key_pem)."""
        key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        csr = (
            x509.CertificateSigningRequestBuilder()
            .subject_name(
                x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, subject_id)])
            )
            .sign(key, hashes.SHA256())
        )
        cert_pem, _ = self.sign_csr(
            csr.public_bytes(serialization.Encoding.PEM).decode(),
            subject_id,
            validity_days,
        )
        key_pem = key.private_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PrivateFormat.TraditionalOpenSSL,
            encryption_algorithm=serialization.NoEncryption(),
        ).decode()
        return cert_pem, key_pem

    @property
    def ca_pem(self) -> str:
        return self._ca_cert_pem
