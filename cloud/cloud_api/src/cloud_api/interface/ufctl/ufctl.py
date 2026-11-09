"""
`ufctl` remote access: device login and device keys (RFD 5, "ufctl connect").

Routes (all under /api/v2/cloud/ufctl):
  POST   /device           ufctl starts a login (RFC 8628)       (no user auth)
  POST   /token            ufctl polls for the device key        (no user auth)
  GET    /device/request   account UI reads a pending request    (user auth)
  POST   /device/approve   owner approves the request            (user auth)
  POST   /device/deny      owner denies the request              (user auth)
  GET    /devices          owner lists their device keys         (user auth)
  DELETE /devices/{id}     owner revokes a device key            (user auth)

The wire contract of /device and /token is
edge/cmd/ufctl/remoteaccess/doc.go. Note that /token reports its errors as a
top-level {"error": "..."} body with status 400 (RFC 8628 section 3.5), which is
what the ufctl client parses; this is NOT the {"detail": ...} envelope the other
routers use.
"""

from fastapi import APIRouter, Depends, HTTPException, status
from fastapi.responses import JSONResponse, Response

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api.ufctl import (
    DEVICE_CODE_GRANT,
    DeviceLoginDecisionRequest,
    DeviceLoginDecisionResponse,
    DeviceLoginRequestInfo,
    ListDeviceKeysResponse,
    UfctlDeviceRequest,
    UfctlDeviceResponse,
    UfctlTokenRequest,
    UfctlTokenResponse,
)
from cloud_api.service.device_key_service import DeviceKeyService
from cloud_api.service.device_login_service import (
    DeviceLoginError,
    DeviceLoginExpiredError,
    DeviceLoginNotFoundError,
    DeviceLoginNotPendingError,
    DeviceLoginService,
    TooManyAttemptsError,
)
from cloud_api.service_manager import get_device_key_service, get_device_login_service

router = APIRouter(prefix="/ufctl", tags=["ufctl"])

_NO_STORE = {"Cache-Control": "no-store", "Pragma": "no-cache"}


def _too_many() -> HTTPException:
    return HTTPException(
        status_code=status.HTTP_429_TOO_MANY_REQUESTS,
        detail="Too many attempts, try again later",
        headers={"Retry-After": "60"},
    )


@router.post(
    "/device",
    response_model=UfctlDeviceResponse,
    summary="Start a ufctl login and obtain a device code (RFC 8628)",
)
async def start_device_login(
    req: UfctlDeviceRequest,
    response: Response,
    svc: DeviceLoginService = Depends(get_device_login_service),
) -> UfctlDeviceResponse:
    response.headers.update(_NO_STORE)
    try:
        return await svc.start(req)
    except TooManyAttemptsError:
        raise _too_many()


@router.post(
    "/token",
    response_model=UfctlTokenResponse,
    summary="Poll for approval and exchange the device code for a device key",
    responses={
        400: {
            "description": "RFC 8628 error: authorization_pending, slow_down, "
            "expired_token, access_denied (or unsupported_grant_type)"
        }
    },
)
async def poll_device_token(
    req: UfctlTokenRequest,
    svc: DeviceLoginService = Depends(get_device_login_service),
):
    if req.grant_type != DEVICE_CODE_GRANT:
        return JSONResponse(
            {"error": "unsupported_grant_type"},
            status_code=status.HTTP_400_BAD_REQUEST,
            headers=_NO_STORE,
        )
    try:
        token = await svc.poll(req.device_code)
    except DeviceLoginError as exc:
        return JSONResponse(
            {"error": exc.code},
            status_code=status.HTTP_400_BAD_REQUEST,
            headers=_NO_STORE,
        )
    # The one response that carries the secret: never cacheable.
    return JSONResponse(token.model_dump(), headers=_NO_STORE)


@router.get(
    "/device/request",
    response_model=DeviceLoginRequestInfo,
    summary="Details of a pending ufctl login for the confirmation page",
)
async def get_device_login_request(
    user_code: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    svc: DeviceLoginService = Depends(get_device_login_service),
) -> DeviceLoginRequestInfo:
    try:
        return await svc.get_request(user_code, claims.azp)
    except DeviceLoginNotFoundError:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "Login request not found")
    except DeviceLoginExpiredError:
        raise HTTPException(status.HTTP_410_GONE, "Login request has expired")
    except TooManyAttemptsError:
        raise _too_many()


async def _decide(decision: str, req, claims, svc) -> DeviceLoginDecisionResponse:
    try:
        return await getattr(svc, decision)(req.user_code, claims.azp)
    except DeviceLoginNotFoundError:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "Login request not found")
    except DeviceLoginExpiredError:
        raise HTTPException(status.HTTP_410_GONE, "Login request has expired")
    except DeviceLoginNotPendingError:
        raise HTTPException(status.HTTP_409_CONFLICT, "Login request already decided")
    except TooManyAttemptsError:
        raise _too_many()


@router.post(
    "/device/approve",
    response_model=DeviceLoginDecisionResponse,
    summary="Approve a ufctl login: binds the request to the caller's account",
)
async def approve_device_login(
    req: DeviceLoginDecisionRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    svc: DeviceLoginService = Depends(get_device_login_service),
) -> DeviceLoginDecisionResponse:
    return await _decide("approve", req, claims, svc)


@router.post(
    "/device/deny",
    response_model=DeviceLoginDecisionResponse,
    summary="Deny a ufctl login",
)
async def deny_device_login(
    req: DeviceLoginDecisionRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    svc: DeviceLoginService = Depends(get_device_login_service),
) -> DeviceLoginDecisionResponse:
    return await _decide("deny", req, claims, svc)


@router.get(
    "/devices",
    response_model=ListDeviceKeysResponse,
    summary="List the caller's active device keys (never the secrets)",
)
async def list_device_keys(
    claims: AccessTokenClaims = Depends(get_access_claims),
    svc: DeviceKeyService = Depends(get_device_key_service),
) -> ListDeviceKeysResponse:
    items = await svc.list_for_account(claims.azp)
    return ListDeviceKeysResponse(items=items, total=len(items))


@router.delete(
    "/devices/{device_key_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    summary="Revoke a device key of the caller's account",
)
async def revoke_device_key(
    device_key_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    svc: DeviceKeyService = Depends(get_device_key_service),
) -> Response:
    if not await svc.revoke(claims.azp, device_key_id):
        raise HTTPException(status.HTTP_404_NOT_FOUND, "Device key not found")
    return Response(status_code=status.HTTP_204_NO_CONTENT)
