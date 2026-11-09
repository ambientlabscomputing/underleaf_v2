"""
cert_lib.py — Utilities for signing device CSRs with the cloud-api root CA.

The root CA private key signs device certificates and the server certificate
nginx presents, and nothing else. (It used to double as the OAuth JWT key; that
is now a separate key, see auth_lib.) Device certificates are issued with
extendedKeyUsage=clientAuth so they can be used for mutual TLS authentication
by registered cluster orchestrators. The server certificate is issued with
extendedKeyUsage=serverAuth and a subjectAltName for every name clusters dial.
"""

import ipaddress
from datetime import UTC, datetime, timedelta

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
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
        dns_names: list[str],
        ip_addresses: list[str] | None = None,
        validity_days: int = 365,
    ) -> tuple[str, str]:
        """Issue a TLS server certificate and return (certificate_pem, key_pem).

        The leaf is signed by the cluster CA, carries serverAuth only (so a
        device client certificate cannot be used as a server, and the reverse),
        is not a CA, and lists every name in subjectAltName. Clusters verify
        the host they dial against those names. The key is a fresh ECDSA P-256
        key that belongs to nginx; the CA key never leaves cloud_api.
        """
        if not dns_names and not ip_addresses:
            raise ValueError("a server certificate needs at least one name")
        key = ec.generate_private_key(ec.SECP256R1())
        now = datetime.now(UTC)
        sans: list[x509.GeneralName] = [x509.DNSName(n) for n in dns_names]
        sans += [x509.IPAddress(ipaddress.ip_address(a)) for a in ip_addresses or []]
        cn = dns_names[0] if dns_names else str(ip_addresses[0])  # type: ignore[index]
        cert = (
            x509.CertificateBuilder()
            .subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, cn)]))
            .issuer_name(self._ca_cert.subject)
            .public_key(key.public_key())
            .serial_number(x509.random_serial_number())
            .not_valid_before(now - timedelta(minutes=5))
            .not_valid_after(now + timedelta(days=validity_days))
            .add_extension(
                x509.BasicConstraints(ca=False, path_length=None), critical=True
            )
            .add_extension(
                x509.KeyUsage(
                    digital_signature=True,
                    content_commitment=False,
                    key_encipherment=False,
                    data_encipherment=False,
                    key_agreement=False,
                    key_cert_sign=False,
                    crl_sign=False,
                    encipher_only=False,
                    decipher_only=False,
                ),
                critical=True,
            )
            .add_extension(
                x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH]),
                critical=False,
            )
            .add_extension(x509.SubjectAlternativeName(sans), critical=False)
            .sign(self._ca_key, hashes.SHA256())
        )
        key_pem = key.private_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PrivateFormat.PKCS8,
            encryption_algorithm=serialization.NoEncryption(),
        ).decode()
        return cert.public_bytes(serialization.Encoding.PEM).decode(), key_pem

    @property
    def ca_cert(self) -> x509.Certificate:
        return self._ca_cert

    @property
    def ca_cert_pem(self) -> str:
        return self._ca_cert_pem
