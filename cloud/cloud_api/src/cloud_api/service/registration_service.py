"""
RegistrationService — orchestrates the RFC 8628 device-authorization flow.

Endpoints handled by this service:
  POST /registration/device        — orch-server registers a ClusterCandidate
  POST /registration/token         — orch-server polls for approval
  GET  /registration/candidate     — account_ui reads candidate details
  POST /registration/approve       — account_ui user confirms the link
  POST /registration/certificate   — orch-server exchanges one-time token for signed cert
"""

import hashlib
import secrets
import string
from datetime import datetime, timedelta, timezone

from cloud_api import app_config, logger
from cloud_api.lib.cert_lib import CertLib
from cloud_api.models.api import (
    ApproveCandidateResponse,
    Cluster,
    ClusterCandidate,
    CandidateStatus,
    CreateClusterRequest,
    DeviceAuthResponse,
    IssueCertificateResponse,
    PollTokenResponse,
    RegisterDeviceRequest,
)
from cloud_api.models.base import generate_id, IDPrefix
from cloud_api.repository.cluster_repository import ClusterRepository
from cloud_api.repository.registration_repository import RegistrationRepository


# ---------------------------------------------------------------------------
# Custom exceptions (translated to HTTP in the router)
# ---------------------------------------------------------------------------

class CandidateNotFoundError(Exception):
    """No candidate found for the given code."""


class CandidateExpiredError(Exception):
    """The device_code or user_code has expired."""


class AuthorizationPendingError(Exception):
    """User has not yet approved — orch-server should keep polling."""


class SlowDownError(Exception):
    """Polling too fast — orch-server must increase its interval."""


class CandidateAlreadyConsumedError(Exception):
    """The one-time token was already used."""


class InvalidOneTimeTokenError(Exception):
    """The provided one-time token does not match or has expired."""


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_DEVICE_CODE_BYTES = 32          # 256-bit device code
_ONE_TIME_TOKEN_BYTES = 32       # 256-bit one-time token
_DEVICE_CODE_TTL_SECONDS = 600   # 10 minutes
_ONE_TIME_TOKEN_TTL_SECONDS = 60 # 1 minute
_POLL_INTERVAL_SECONDS = 5
_MIN_POLL_INTERVAL_SECONDS = 5   # guard against fast pollers


def _sha256(value: str) -> str:
    return hashlib.sha256(value.encode()).hexdigest()


def _generate_user_code() -> str:
    """Generate an 8-char uppercase alphanumeric user code (XXXX-XXXX)."""
    alphabet = string.ascii_uppercase + string.digits
    part = lambda: "".join(secrets.choice(alphabet) for _ in range(4))
    return f"{part()}-{part()}"


# ---------------------------------------------------------------------------
# Service
# ---------------------------------------------------------------------------

class RegistrationService:
    def __init__(
        self,
        registration_repo: RegistrationRepository,
        cluster_repo: ClusterRepository,
        cert_lib: CertLib,
    ) -> None:
        self.registration_repo = registration_repo
        self.cluster_repo = cluster_repo
        self.cert_lib = cert_lib

    def _verification_uri(self) -> str:
        if not app_config:
            raise RuntimeError("App config not loaded")
        return f"{app_config.account_ui_base_url.rstrip('/')}/clusters/register"

    async def register_device(self, req: RegisterDeviceRequest) -> DeviceAuthResponse:
        """Create a ClusterCandidate and return the device-auth response."""
        candidate_id = generate_id(IDPrefix.CANDIDATE)
        device_code = secrets.token_hex(_DEVICE_CODE_BYTES)
        user_code = _generate_user_code()
        device_code_hash = _sha256(device_code)
        now = datetime.now(timezone.utc)
        expires_at = now + timedelta(seconds=_DEVICE_CODE_TTL_SECONDS)

        await self.registration_repo.create_candidate(
            id=candidate_id,
            user_code=user_code,
            device_code_hash=device_code_hash,
            proposed_cluster_name=req.proposed_cluster_name,
            proposed_cluster_id=req.proposed_cluster_id,
            device_code_expires_at=expires_at,
            poll_interval=_POLL_INTERVAL_SECONDS,
        )

        logger.bind(candidate_id=candidate_id, user_code=user_code).info(
            "ClusterCandidate created"
        )

        uri = self._verification_uri()
        return DeviceAuthResponse(
            device_code=device_code,
            user_code=user_code,
            verification_uri=uri,
            verification_uri_complete=f"{uri}?user_code={user_code}",
            expires_in=_DEVICE_CODE_TTL_SECONDS,
            interval=_POLL_INTERVAL_SECONDS,
        )

    async def poll_token(self, device_code: str) -> PollTokenResponse:
        """Poll for approval; returns one_time_cluster_token when approved."""
        device_code_hash = _sha256(device_code)
        candidate = await self.registration_repo.get_by_device_code_hash(device_code_hash)

        if candidate is None:
            raise CandidateNotFoundError("Unknown device_code")

        now = datetime.now(timezone.utc)

        # Expire check
        expires = candidate.device_code_expires_at
        if expires.tzinfo is None:
            expires = expires.replace(tzinfo=timezone.utc)
        if now > expires:
            raise CandidateExpiredError("device_code has expired")

        # Slow-down guard
        if candidate.last_polled_at is not None:
            last = candidate.last_polled_at
            if last.tzinfo is None:
                last = last.replace(tzinfo=timezone.utc)
            elapsed = (now - last).total_seconds()
            if elapsed < _MIN_POLL_INTERVAL_SECONDS:
                raise SlowDownError("Polling too fast")

        await self.registration_repo.touch_poll(candidate.id)

        if candidate.status == "consumed":
            raise CandidateAlreadyConsumedError("Already consumed")

        if candidate.status == "pending":
            raise AuthorizationPendingError("Awaiting user approval")

        if candidate.status == "approved":
            # Issue the one-time token now (it was already set on approval)
            one_time_token = secrets.token_hex(_ONE_TIME_TOKEN_BYTES)
            # We store the hash; re-minting would break the hash. Return the
            # stored plaintext from creation — instead, we store it during
            # approve() and return it here.
            #
            # The one_time_token_hash is stored; we cannot recover the plaintext.
            # Solution: store the plaintext one_time_token in the candidate during
            # approve, encrypted or simply returned once. For simplicity we mint
            # a *new* one-time token here and re-hash it (the approve endpoint
            # sets a well-known sentinel so we know it is ready).
            new_token = secrets.token_hex(_ONE_TIME_TOKEN_BYTES)
            new_hash = _sha256(new_token)
            new_expires = now + timedelta(seconds=_ONE_TIME_TOKEN_TTL_SECONDS)

            await self.registration_repo.approve(
                candidate_id=candidate.id,
                principal_account_id=candidate.principal_account_id or "",
                cluster_id=candidate.cluster_id or "",
                one_time_token_hash=new_hash,
                one_time_token_expires_at=new_expires,
            )

            logger.bind(candidate_id=candidate.id).info(
                "One-time cluster token issued"
            )
            return PollTokenResponse(
                one_time_cluster_token=new_token,
                cluster_id=candidate.cluster_id or "",
            )

        raise CandidateNotFoundError(f"Unexpected status: {candidate.status}")

    async def get_candidate(self, user_code: str) -> ClusterCandidate:
        """Return candidate details for the account_ui confirmation panel."""
        candidate = await self.registration_repo.get_by_user_code(user_code)
        if candidate is None:
            raise CandidateNotFoundError(f"No candidate for user_code={user_code}")

        now = datetime.now(timezone.utc)
        expires = candidate.device_code_expires_at
        if expires.tzinfo is None:
            expires = expires.replace(tzinfo=timezone.utc)
        if now > expires:
            raise CandidateExpiredError("Registration has expired")

        return ClusterCandidate(
            id=candidate.id,
            user_code=candidate.user_code,
            status=CandidateStatus(candidate.status),
            proposed_cluster_name=candidate.proposed_cluster_name,
            proposed_cluster_id=candidate.proposed_cluster_id,
            principal_account_id=candidate.principal_account_id,
            cluster_id=candidate.cluster_id,
            created_at=candidate.created_at,
            updated_at=candidate.updated_at,
        )

    async def approve_candidate(
        self, user_code: str, principal_account_id: str
    ) -> ApproveCandidateResponse:
        """Link cluster to principal account and mark candidate as approved."""
        candidate = await self.registration_repo.get_by_user_code(user_code)
        if candidate is None:
            raise CandidateNotFoundError(f"No candidate for user_code={user_code}")

        now = datetime.now(timezone.utc)
        expires = candidate.device_code_expires_at
        if expires.tzinfo is None:
            expires = expires.replace(tzinfo=timezone.utc)
        if now > expires:
            raise CandidateExpiredError("Registration has expired")

        if candidate.status != "pending":
            raise CandidateNotFoundError(f"Candidate is not pending (status={candidate.status})")

        # Create the actual Cluster resource
        cluster = await self.cluster_repo.create_cluster(
            CreateClusterRequest(
                id=candidate.proposed_cluster_id,
                name=candidate.proposed_cluster_name,
                principal_account_id=principal_account_id,
            )
        )

        # Mark approved with a sentinel hash (real token minted on first poll)
        await self.registration_repo.approve(
            candidate_id=candidate.id,
            principal_account_id=principal_account_id,
            cluster_id=cluster.id,
            one_time_token_hash="__pending__",
            one_time_token_expires_at=now + timedelta(seconds=_DEVICE_CODE_TTL_SECONDS),
        )

        logger.bind(candidate_id=candidate.id, cluster_id=cluster.id).info(
            "Candidate approved, cluster created"
        )
        return ApproveCandidateResponse(cluster=cluster)

    async def issue_certificate(
        self, one_time_token: str, csr_pem: str
    ) -> IssueCertificateResponse:
        """Sign the CSR if the one-time token is valid, then consume the candidate."""
        token_hash = _sha256(one_time_token)
        # We need to look up by one_time_token_hash — add a helper in repo
        from sqlalchemy import select
        from cloud_api.models.sql import SQLClusterCandidate
        from cloud_api.repository.base_repository import BaseRepository

        # Use the repo's session directly via a small inline query
        candidate = None
        async with self.registration_repo.get_session() as session:
            from sqlalchemy import select as sa_select
            result = await session.scalars(
                sa_select(SQLClusterCandidate).where(
                    SQLClusterCandidate.one_time_token_hash == token_hash,
                    SQLClusterCandidate.status == "approved",
                )
            )
            candidate = result.first()

        if candidate is None:
            raise InvalidOneTimeTokenError("Invalid or already-used token")

        now = datetime.now(timezone.utc)
        expires = candidate.one_time_token_expires_at
        if expires and expires.tzinfo is None:
            expires = expires.replace(tzinfo=timezone.utc)
        if expires is None or now > expires:
            raise InvalidOneTimeTokenError("One-time token has expired")

        cluster_id = candidate.cluster_id or candidate.proposed_cluster_id
        cert_pem, ca_chain_pem = self.cert_lib.sign_csr(csr_pem, cluster_id)

        await self.registration_repo.consume(candidate.id)

        logger.bind(candidate_id=candidate.id, cluster_id=cluster_id).info(
            "Certificate issued, candidate consumed"
        )
        return IssueCertificateResponse(
            certificate_pem=cert_pem,
            ca_chain_pem=ca_chain_pem,
        )
