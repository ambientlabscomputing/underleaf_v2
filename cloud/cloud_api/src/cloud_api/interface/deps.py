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

from functools import lru_cache

from fastapi import Depends, HTTPException, Request, status
from fastapi.security import APIKeyHeader
from pydantic import Field

from cloud_api import logger
from cloud_api.lib.auth_lib import AuthLib, InvalidTokenError, JWTClaims
from cloud_api.models.api import Cluster, Node
from cloud_api.repository.user_repository import UserRepository
from cloud_api.service_manager import (
    get_auth_lib,
    get_cluster_repo,
    get_node_repo,
    get_user_repo,
)

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


@lru_cache
def get_auth_lib(user_repo: UserRepository):
    return AuthLib(user_repo=user_repo)


@lru_cache
def mint_token(
    auth_lib: AuthLib,
    principal_account_id: str,
    node_id: str | None = None,
    cluster_id: str | None = None,
) -> str:
    user_id = node_id or cluster_id or ""
    return auth_lib.mint_access_token(
        user_id=user_id,
        principal_account_id=principal_account_id,
    )


async def fetch_data_for_node_or_cluster(
    subject_id: str,
) -> tuple[Node | None, Cluster]:
    node: Node | None = None
    cluster: Cluster | None = None
    if subject_id.startswith("node_"):
        node_repo = get_node_repo()
        node = await node_repo.get_node(subject_id)
        if not node:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail=f"Node with ID {subject_id} not found",
            )
        cluster = await get_cluster_repo().get_cluster(node.cluster_id)
        if not cluster:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail=f"Cluster with ID {node.cluster_id} not found",
            )
    elif subject_id.startswith("cluster_"):
        cluster = await get_cluster_repo().get_cluster(subject_id)
        if not cluster:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail=f"Cluster with ID {subject_id} not found",
            )
    else:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail=f"Invalid X-Subject-Id header value: {subject_id}",
        )

    return node, cluster


async def x_subject_id_token_middleware_(request: Request, call_next):
    if "X-Subject-Id" not in request.headers:
        # skip this helper if the header is not present
        logger.debug("X-Subject-Id header not present, skipping token minting")
    elif request.headers.get("Authorization"):
        # skip this helper if the Authorization header is already present
        logger.debug("Authorization header already present, skipping token minting")
    else:
        logger.debug("X-Subject-Id header present, minting token for request")
        subject_id = request.headers["X-Subject-Id"]
        node, cluster = await fetch_data_for_node_or_cluster(subject_id)
        if not cluster:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Cluster not found",
            )

        auth_lib = get_auth_lib(get_user_repo())
        token = mint_token(
            auth_lib,
            principal_account_id=cluster.principal_account_id,
            node_id=node.id if node else None,
            cluster_id=cluster.id,
        )
        logger.debug("Minted token for request", extra={"token": token[:20]})
        # ASGI header names in the raw (name, value) tuple list must be
        # lowercase (per the ASGI spec) -- Starlette's Headers lookups don't
        # re-normalize case when scanning this list, so a capitalized name
        # here is silently invisible to every downstream `request.headers`
        # read, including the APIKeyHeader dependency this exists to satisfy.
        request.headers.__dict__["_list"].append(
            (b"authorization", f"Bearer {token}".encode())
        )
        logger.debug(
            "Added Authorization header to request",
            extra={"token": request.headers.get("Authorization", "" * 20)[:20]},
        )

    response = await call_next(request)
    return response
