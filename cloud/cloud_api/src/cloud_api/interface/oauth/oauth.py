from fastapi import APIRouter, Depends, Form, HTTPException, status
from fastapi.security import APIKeyHeader
from pydantic import BaseModel

from cloud_api import app_config
from cloud_api.lib.auth_lib import InvalidCredentialsError, InvalidTokenError
from cloud_api.models.api import SignInRequest, User
from cloud_api.service.auth_service import AuthService
from cloud_api.service_manager import get_auth_service

router = APIRouter(prefix="/oauth", tags=["OAuth"])

# APIKeyHeader sends the token via the Authorization header and renders
# a single token field in Swagger (no username/password form).
api_key_scheme = APIKeyHeader(name="Authorization", auto_error=True)


# ---------------------------------------------------------------------------
# OAuth 2.0 token response (RFC 6749 §5.1)
# ---------------------------------------------------------------------------
class OAuth2TokenResponse(BaseModel):
    access_token: str
    token_type: str = "bearer"
    expires_in: int  # seconds
    refresh_token: str


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _access_token_ttl_seconds() -> int:
    if not app_config:
        raise RuntimeError("App config not loaded")
    return app_config.oauth.access_token_expiration_minutes * 60


# ---------------------------------------------------------------------------
# Endpoints
# ---------------------------------------------------------------------------


@router.post(
    "/token",
    response_model=OAuth2TokenResponse,
    summary="Issue or refresh an access token",
)
async def token(
    grant_type: str = Form(..., description="'password' or 'refresh_token'"),
    # password grant fields
    username: str | None = Form(
        default=None, description="User email (password grant)"
    ),
    password: str | None = Form(
        default=None, description="User password (password grant)"
    ),
    # refresh_token grant fields
    refresh_token: str | None = Form(
        default=None, description="Refresh token (refresh_token grant)"
    ),
    auth_service: AuthService = Depends(get_auth_service),
) -> OAuth2TokenResponse:
    if grant_type == "password":
        if not username or not password:
            raise HTTPException(
                status_code=status.HTTP_400_BAD_REQUEST,
                detail={
                    "error": "invalid_request",
                    "error_description": "username and password are required",
                },
            )
        try:
            token_resp = await auth_service.login(
                SignInRequest(email=username, password=password)
            )
        except InvalidCredentialsError:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail={
                    "error": "invalid_grant",
                    "error_description": "Invalid credentials",
                },
                headers={"WWW-Authenticate": "Bearer"},
            )
        return OAuth2TokenResponse(
            access_token=token_resp.access_token,
            refresh_token=token_resp.refresh_token,
            expires_in=_access_token_ttl_seconds(),
        )

    elif grant_type == "refresh_token":
        if not refresh_token:
            raise HTTPException(
                status_code=status.HTTP_400_BAD_REQUEST,
                detail={
                    "error": "invalid_request",
                    "error_description": "refresh_token is required",
                },
            )
        try:
            token_resp = await auth_service.refresh_token(refresh_token)
        except InvalidTokenError:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail={
                    "error": "invalid_grant",
                    "error_description": "Token is invalid or expired",
                },
                headers={"WWW-Authenticate": "Bearer"},
            )
        return OAuth2TokenResponse(
            access_token=token_resp.access_token,
            refresh_token=token_resp.refresh_token,
            expires_in=_access_token_ttl_seconds(),
        )

    else:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail={
                "error": "unsupported_grant_type",
                "error_description": f"Unsupported grant_type: {grant_type}",
            },
        )


@router.get(
    "/userinfo",
    response_model=User,
    summary="Return claims for the authenticated user (OIDC UserInfo)",
)
async def userinfo(
    authorization: str = Depends(api_key_scheme),
    auth_service: AuthService = Depends(get_auth_service),
) -> User:
    # Strip "Bearer " prefix if present so callers can send either form.
    token = authorization.removeprefix("Bearer ").strip()
    try:
        return await auth_service.whoami(token)
    except InvalidTokenError:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail={
                "error": "invalid_token",
                "error_description": "Token is invalid or expired",
            },
            headers={"WWW-Authenticate": 'Bearer error="invalid_token"'},
        )
