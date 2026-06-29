"""
Registration router — RFC 8628 device authorization grant for cluster onboarding.

Routes (all under /api/v2/registration):
  POST /device        — orch-server registers a cluster candidate (no user auth)
  POST /token         — orch-server polls for approval (no user auth)
  GET  /candidate     — account_ui reads candidate details (user auth)
  POST /approve       — account_ui user confirms the link (user auth)
  POST /certificate   — orch-server exchanges one-time token for signed cert
"""

from fastapi import APIRouter, Depends, HTTPException, status
from fastapi.security import APIKeyHeader

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    ApproveCandidateRequest,
    ApproveCandidateResponse,
    ClusterCandidate,
    DeviceAuthResponse,
    IssueCertificateRequest,
    IssueCertificateResponse,
    PollTokenRequest,
    PollTokenResponse,
    RegisterDeviceRequest,
)
from cloud_api.service.registration_service import (
    AuthorizationPendingError,
    CandidateAlreadyConsumedError,
    CandidateExpiredError,
    CandidateNotFoundError,
    InvalidOneTimeTokenError,
    RegistrationService,
    SlowDownError,
)
from cloud_api.service_manager import get_registration_service

router = APIRouter(prefix="/registration", tags=["Registration"])

# ---------------------------------------------------------------------------
# One-time-token auth: orch-server presents X-Cluster-Token header
# ---------------------------------------------------------------------------
_cluster_token_header = APIKeyHeader(name="X-Cluster-Token", auto_error=True)


# ---------------------------------------------------------------------------
# Endpoints
# ---------------------------------------------------------------------------


@router.post(
    "/device",
    response_model=DeviceAuthResponse,
    status_code=status.HTTP_201_CREATED,
    summary="Register a cluster candidate and obtain a device code (RFC 8628)",
)
async def register_device(
    req: RegisterDeviceRequest,
    svc: RegistrationService = Depends(get_registration_service),
) -> DeviceAuthResponse:
    return await svc.register_device(req)


@router.post(
    "/token",
    response_model=PollTokenResponse,
    summary="Poll for user approval and exchange device code for a one-time cluster token",
)
async def poll_token(
    req: PollTokenRequest,
    svc: RegistrationService = Depends(get_registration_service),
) -> PollTokenResponse:
    try:
        return await svc.poll_token(req.device_code)
    except CandidateNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail={
                "error": "invalid_grant",
                "error_description": "Unknown device_code",
            },
        )
    except CandidateExpiredError:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail={
                "error": "expired_token",
                "error_description": "device_code has expired",
            },
        )
    except AuthorizationPendingError:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail={
                "error": "authorization_pending",
                "error_description": "User has not yet approved",
            },
        )
    except SlowDownError:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail={"error": "slow_down", "error_description": "Polling too fast"},
        )
    except CandidateAlreadyConsumedError:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail={"error": "invalid_grant", "error_description": "Already consumed"},
        )


@router.get(
    "/candidate",
    response_model=ClusterCandidate,
    summary="Get cluster candidate details for the confirmation panel",
)
async def get_candidate(
    user_code: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    svc: RegistrationService = Depends(get_registration_service),
) -> ClusterCandidate:
    try:
        return await svc.get_candidate(user_code)
    except CandidateNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Candidate not found"
        )
    except CandidateExpiredError:
        raise HTTPException(
            status_code=status.HTTP_410_GONE, detail="Registration has expired"
        )


@router.post(
    "/approve",
    response_model=ApproveCandidateResponse,
    summary="Approve cluster registration — links cluster to authenticated user's principal account",
)
async def approve_candidate(
    req: ApproveCandidateRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    svc: RegistrationService = Depends(get_registration_service),
) -> ApproveCandidateResponse:
    try:
        return await svc.approve_candidate(req.user_code, claims.azp)
    except CandidateNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Candidate not found"
        )
    except CandidateExpiredError:
        raise HTTPException(
            status_code=status.HTTP_410_GONE, detail="Registration has expired"
        )


@router.post(
    "/certificate",
    response_model=IssueCertificateResponse,
    summary="Exchange one-time cluster token for a signed mTLS client certificate",
)
async def issue_certificate(
    req: IssueCertificateRequest,
    one_time_token: str = Depends(_cluster_token_header),
    svc: RegistrationService = Depends(get_registration_service),
) -> IssueCertificateResponse:
    try:
        return await svc.issue_certificate(one_time_token, req.csr_pem, req.node_id)
    except InvalidOneTimeTokenError as exc:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail={"error": "invalid_token", "error_description": str(exc)},
        )
