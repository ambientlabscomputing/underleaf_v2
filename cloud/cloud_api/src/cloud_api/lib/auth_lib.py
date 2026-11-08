import os
import pathlib
from datetime import UTC, datetime, timedelta

import jwt
from argon2 import PasswordHasher
from argon2.exceptions import InvalidHashError, VerificationError, VerifyMismatchError
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives.asymmetric.rsa import RSAPrivateKey, RSAPublicKey
from pydantic import BaseModel, Field

from cloud_api import app_config, logger
from cloud_api.models.api import SignInRequest, SignInResponse
from cloud_api.repository.user_repository import UserRepository

_ph = PasswordHasher()


class InvalidCredentialsError(Exception):
    """Raised when email/password combination cannot be verified."""


class InvalidTokenError(Exception):
    """Raised when a JWT cannot be validated."""


class JWTClaims(BaseModel):
    sub: str = Field(..., description="Subject of the token, typically the user ID")
    azp: str | None = Field(
        default=None,
        description="Authorized party — the principal account ID (access tokens only)",
    )
    exp: int = Field(..., description="Expiration time as a Unix timestamp")
    iat: int = Field(..., description="Issued at time as a Unix timestamp")
    iss: str = Field(..., description="Issuer of the token")
    aud: str = Field(..., description="Audience for the token")


class AuthLib:
    _private_key: RSAPrivateKey
    _public_key: RSAPublicKey

    def __init__(self, user_repo: UserRepository):
        self.user_repo = user_repo
        self.bootstrap_servers()

    def bootstrap_servers(self):
        if not app_config:
            raise RuntimeError("App config not loaded")
        if not app_config.oauth.bootstrap_certs:
            logger.info("Certificate bootstrapping disabled in config, skipping")
            return
        logger.info("Bootstrapping OAuth certificates")
        cert_path = app_config.oauth.root_ca_cert_path
        key_path = app_config.oauth.private_key_path
        jwt_key_path = app_config.oauth.jwt_private_key_path
        self.__bootstrap_certs(cert_path, key_path)
        self.bootstrap_jwt_key(jwt_key_path, key_path)
        self._load_keys(jwt_key_path, key_path)

    @staticmethod
    def bootstrap_jwt_key(jwt_key_path: str, ca_key_path: str) -> bool:
        """Create the OAuth JWT signing key if it does not exist.

        The JWT key is its own RSA key. It must never be the cluster CA key:
        the CA key signs device certificates and, if it also signed tokens, a
        leak of either would compromise both. Returns True if a key was
        generated. Generating one invalidates every token signed by a previous
        key, so on an existing deployment (where the CA key used to sign
        tokens) every user has to sign in again once.
        """
        path = pathlib.Path(jwt_key_path)
        if os.path.abspath(jwt_key_path) == os.path.abspath(ca_key_path):
            raise RuntimeError(
                "oauth.jwt_private_key_path must differ from oauth.private_key_path: "
                "the JWT signing key may not be the cluster CA key"
            )
        if path.exists():
            return False
        logger.warning(
            f"No OAuth JWT signing key at {jwt_key_path}: generating a new one. "
            "Tokens signed by any earlier key (including the old shared CA/JWT "
            "key) no longer verify, so users sign in again."
        )
        key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        path.parent.mkdir(parents=True, exist_ok=True)
        # 0600 from the first byte, not chmod-after-write.
        fd = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "wb") as f:
            f.write(
                key.private_bytes(
                    encoding=serialization.Encoding.PEM,
                    format=serialization.PrivateFormat.PKCS8,
                    encryption_algorithm=serialization.NoEncryption(),
                )
            )
        return True

    def _load_keys(self, jwt_key_path: str, ca_key_path: str | None = None) -> None:
        with open(jwt_key_path, "rb") as f:
            private_key = serialization.load_pem_private_key(f.read(), password=None)
        assert isinstance(private_key, RSAPrivateKey), "Expected RSA private key"
        if ca_key_path is not None and os.path.exists(ca_key_path):
            with open(ca_key_path, "rb") as f:
                ca_key = serialization.load_pem_private_key(f.read(), password=None)
            if isinstance(ca_key, RSAPrivateKey) and (
                ca_key.public_key().public_numbers()
                == private_key.public_key().public_numbers()
            ):
                raise RuntimeError(
                    f"{jwt_key_path} is the same key as the cluster CA key "
                    f"{ca_key_path}. Delete {jwt_key_path} to have a separate JWT "
                    "key generated (this signs every user out once)."
                )
        self._private_key = private_key
        self._public_key = private_key.public_key()

    def mint_token(self, claims: JWTClaims) -> str:
        return jwt.encode(claims.model_dump(), self._private_key, algorithm="RS256")

    def mint_access_token(self, user_id: str, principal_account_id: str) -> str:
        if not app_config:
            raise RuntimeError("App config not loaded")
        now = datetime.now(UTC)
        claims = JWTClaims(
            sub=user_id,
            azp=principal_account_id,
            iat=int(now.timestamp()),
            exp=int(
                (
                    now
                    + timedelta(
                        minutes=app_config.oauth.access_token_expiration_minutes
                    )
                ).timestamp()
            ),
            iss=app_config.oauth.issuer,
            aud=app_config.oauth.audience,
        )
        return self.mint_token(claims)

    def mint_refresh_token(self, user_id: str) -> str:
        if not app_config:
            raise RuntimeError("App config not loaded")
        now = datetime.now(UTC)
        claims = JWTClaims(
            sub=user_id,
            iat=int(now.timestamp()),
            exp=int(
                (
                    now + timedelta(days=app_config.oauth.refresh_token_expiration_days)
                ).timestamp()
            ),
            iss=app_config.oauth.issuer,
            aud=app_config.oauth.audience,
        )
        return self.mint_token(claims)

    def validate_token(self, token: str) -> JWTClaims:
        if not app_config:
            raise RuntimeError("App config not loaded")
        try:
            payload = jwt.decode(
                token,
                self._public_key,
                algorithms=["RS256"],
                audience=app_config.oauth.audience,
                issuer=app_config.oauth.issuer,
            )
            return JWTClaims(**payload)
        except jwt.ExpiredSignatureError:
            raise InvalidTokenError("Token has expired")
        except jwt.InvalidTokenError as e:
            raise InvalidTokenError(f"Invalid token: {e}")

    async def sign_in(self, req: SignInRequest) -> SignInResponse:
        # Look up by email first. Use the same error for "not found" and
        # "wrong password" to avoid leaking whether an email is registered.
        user = await self.user_repo.get_user_by_email(req.email)
        if user is None:
            raise InvalidCredentialsError("Invalid credentials")

        password_hash = await self.user_repo.get_user_password_hash(user.id)
        if password_hash is None:
            raise InvalidCredentialsError("Invalid credentials")

        try:
            _ph.verify(password_hash, req.password)
        except (VerifyMismatchError, VerificationError, InvalidHashError):
            raise InvalidCredentialsError("Invalid credentials")

        return SignInResponse(user=user)

    def __bootstrap_certs(self, cert_path: str, key_path: str):
        # Check if cert and key already exist
        try:
            with open(cert_path, "rb") as cert_file, open(key_path, "rb") as key_file:
                x509.load_pem_x509_certificate(cert_file.read())
                serialization.load_pem_private_key(key_file.read(), password=None)
                logger.info("Bootstrap certs already exist, skipping generation")
                return
        except FileNotFoundError:
            logger.info("Bootstrap certs not found, generating new ones")

        # Generate new RSA private key
        private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)

        # Create a self-signed x509 certificate
        now = datetime.now(UTC)
        subject = issuer = x509.Name(
            [
                x509.NameAttribute(x509.NameOID.COUNTRY_NAME, "US"),
                x509.NameAttribute(x509.NameOID.STATE_OR_PROVINCE_NAME, "California"),
                x509.NameAttribute(x509.NameOID.LOCALITY_NAME, "San Francisco"),
                x509.NameAttribute(x509.NameOID.ORGANIZATION_NAME, "Underleaf"),
                x509.NameAttribute(x509.NameOID.COMMON_NAME, "Underleaf OAuth Root CA"),
            ]
        )
        cert = (
            x509.CertificateBuilder()
            .subject_name(subject)
            .issuer_name(issuer)
            .public_key(private_key.public_key())
            .serial_number(x509.random_serial_number())
            .not_valid_before(now)
            .not_valid_after(now + timedelta(days=3650))  # 10 year validity
            .add_extension(
                x509.BasicConstraints(ca=True, path_length=None), critical=True
            )
            .sign(private_key, hashes.SHA256())
        )

        # Ensure parent directories exist
        pathlib.Path(cert_path).parent.mkdir(parents=True, exist_ok=True)
        pathlib.Path(key_path).parent.mkdir(parents=True, exist_ok=True)

        # Write the private key and certificate to disk
        with open(cert_path, "wb") as cert_file:
            cert_file.write(cert.public_bytes(serialization.Encoding.PEM))
        with open(key_path, "wb") as key_file:
            key_file.write(
                private_key.private_bytes(
                    encoding=serialization.Encoding.PEM,
                    format=serialization.PrivateFormat.TraditionalOpenSSL,
                    encryption_algorithm=serialization.NoEncryption(),
                )
            )
        logger.info("Bootstrap certs generated and saved to disk")
