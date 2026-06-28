"""
Shared FastAPI dependencies for protected endpoints.

Usage:
    from cloud_api.interface.deps import get_claims
    from cloud_api.lib.auth_lib import JWTClaims

    @router.get("/something")
    async def handler(claims: JWTClaims = Depends(get_claims)):
        principal_account_id = claims.azp
        ...
"""

from pydantic import Field
from fastapi import Depends, HTTPException, status
from fastapi.security import APIKeyHeader

from cloud_api.lib.auth_lib import AuthLib, InvalidTokenError, JWTClaims
from cloud_api.service_manager import get_auth_lib

# Reads the Authorization header and surfaces a single token field in Swagger.
_auth_header = APIKeyHeader(name="Authorization", auto_error=True)


async def get_claims(
    authorization: str = Depends(_auth_header),
    auth_lib: AuthLib = Depends(get_auth_lib),
) -> JWTClaims:
    """Validate the Bearer token and return its decoded claims.

    Raises HTTP 401 if the token is absent, expired, or invalid.
    """
    token = authorization.removeprefix("Bearer ").strip()
    try:
        return auth_lib.validate_token(token)
    except InvalidTokenError as exc:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail={"error": "invalid_token", "error_description": str(exc)},
            headers={"WWW-Authenticate": 'Bearer error="invalid_token"'},
        )


class AccessTokenClaims(JWTClaims):
    """JWTClaims narrowed to access tokens — azp is guaranteed non-None.

    Use Depends(get_access_claims) on any endpoint that needs the caller's
    principal_account_id. Refresh tokens (which carry no azp) are rejected
    with HTTP 403 before the handler runs.
    """

    azp: str = Field(...)  # type: ignore[assignment]  # narrows str | None → str


async def get_access_claims(
    claims: JWTClaims = Depends(get_claims),
) -> AccessTokenClaims:
    """Extends get_claims — additionally requires an access token (azp present)."""
    if not claims.azp:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail={"error": "forbidden", "error_description": "Access token required"},
        )
    return AccessTokenClaims(**claims.model_dump())
