"""tls_lib.py: provision the server certificate nginx presents.

nginx used to present the cluster CA certificate itself, with the CA private key
as its TLS key: no host names, and the key that signs every device certificate
sitting in the web server. Now cloud_api issues nginx a normal server
certificate from the CA, publishes it with the public CA certificate in a
directory nginx can read, and keeps the CA key to itself.

The directory (config tls.dir) holds exactly:
  root_ca_cert.pem   public CA certificate (client verification, pinned trust)
  server_cert.pem    server certificate, signed by the CA, with SANs
  server_key.pem     the server certificate's own key (0600)
"""

import ipaddress
import os
import pathlib
from datetime import UTC, datetime, timedelta

from cryptography import x509
from cryptography.exceptions import InvalidSignature, UnsupportedAlgorithm
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import ec

from cloud_api import logger
from cloud_api.config import TLSConfig
from cloud_api.lib.cert_lib import CertLib

CA_CERT_FILE = "root_ca_cert.pem"
SERVER_CERT_FILE = "server_cert.pem"
SERVER_KEY_FILE = "server_key.pem"


def _write(path: pathlib.Path, data: str, mode: int) -> None:
    """Write atomically with the final permissions from the first byte."""
    tmp = path.with_name(path.name + ".tmp")
    fd = os.open(str(tmp), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, mode)
    with os.fdopen(fd, "w") as f:
        f.write(data)
    os.chmod(tmp, mode)
    os.replace(tmp, path)


def _names_of(cert: x509.Certificate) -> set[str]:
    san = cert.extensions.get_extension_for_class(x509.SubjectAlternativeName).value
    names = {f"dns:{n}" for n in san.get_values_for_type(x509.DNSName)}
    names |= {f"ip:{ip}" for ip in san.get_values_for_type(x509.IPAddress)}
    return names


def wanted_names(cfg: TLSConfig) -> set[str]:
    return {f"dns:{n}" for n in cfg.server_names} | {
        f"ip:{ipaddress.ip_address(a)}" for a in cfg.server_ips
    }


def _reason_to_reissue(
    cfg: TLSConfig, ca: x509.Certificate, d: pathlib.Path, now: datetime
) -> str | None:
    """Why the current certificate cannot be kept, or None if it can."""
    try:
        cert = x509.load_pem_x509_certificate((d / SERVER_CERT_FILE).read_bytes())
        key = serialization.load_pem_private_key(
            (d / SERVER_KEY_FILE).read_bytes(), password=None
        )
    except (FileNotFoundError, ValueError):
        return "missing or unreadable"
    pub = key.public_key()
    if not isinstance(pub, ec.EllipticCurvePublicKey) or (
        pub.public_numbers() != cert.public_key().public_numbers()  # type: ignore[union-attr]
    ):
        return "key does not match certificate"
    try:
        cert.verify_directly_issued_by(ca)
    except (ValueError, TypeError, InvalidSignature, UnsupportedAlgorithm):
        return "not issued by the current CA"
    if cert.not_valid_after_utc - now < timedelta(days=cfg.renew_before_days):
        return "expires soon"
    try:
        if _names_of(cert) != wanted_names(cfg):
            return "names differ from configuration"
    except x509.ExtensionNotFound:
        return "no subjectAltName"
    return None


def ensure_server_cert(
    cert_lib: CertLib, cfg: TLSConfig, now: datetime | None = None
) -> bool:
    """Make sure cfg.dir holds a valid server certificate. Returns True if one
    was (re)issued. Always refreshes the public CA certificate copy."""
    now = now or datetime.now(UTC)
    d = pathlib.Path(cfg.dir)
    d.mkdir(parents=True, exist_ok=True)
    os.chmod(d, 0o755)
    _write(d / CA_CERT_FILE, cert_lib.ca_cert_pem, 0o644)
    reason = _reason_to_reissue(cfg, cert_lib.ca_cert, d, now)
    if reason is None:
        return False
    logger.info(f"Issuing nginx server certificate ({reason})")
    cert_pem, key_pem = cert_lib.issue_server_cert(
        cfg.server_names, cfg.server_ips, cfg.validity_days
    )
    # Key first: a reader never pairs a new certificate with an old key.
    _write(d / SERVER_KEY_FILE, key_pem, 0o600)
    _write(d / SERVER_CERT_FILE, cert_pem, 0o644)
    return True
