"""
Remote-access authentication for nginx's auth_request (RFD 5, "Remote access").

  GET /api/v2/cloud/internal/remote-auth

nginx calls this for every request to /clusters/{cluster_id}/remote/..., passing
the caller's `Authorization: Bearer <credential>` and its own
`X-Underleaf-Cluster: <cluster_id>` (taken from the URL, never from the client).

Credential: a `ufctl` device key (`ufdk_...`) or an account access JWT
(Cockpit). Anything else is a 401.

Answers (the body is always empty, so nothing about why a request failed can
leak, and nothing client-supplied is ever echoed):

  200  authorized. Headers:
         X-Underleaf-Assertion:  a fresh ~60 s assertion for this cluster
         X-Underleaf-Connection: the cluster's standing `remote-access`
                                 connection id, or "" when it has none
  400  the request carries X-Subject-Id (or has no usable header set at all)
  401  missing, malformed, unknown, revoked or expired credential; a refresh
       token; a token minted from X-Subject-Id
  403  the cluster is missing, malformed, or not the caller's. ONE answer for
       all of them, so cluster ids cannot be probed.

This route is an internal one: nginx must not expose /internal/ to clients and
must set X-Underleaf-Cluster itself. Authentication here does not rely on that:
it never accepts a credential minted from X-Subject-Id, and X-Subject-Id is
refused outright.
"""

import re

from fastapi import APIRouter, Depends, Request, Response, status

from cloud_api import logger
from cloud_api.service.assertion_service import ClusterAccessDeniedError
from cloud_api.service.remote_auth_service import RemoteAuthService
from cloud_api.service_manager import get_remote_auth_service

router = APIRouter(prefix="/internal", tags=["Internal"])

ASSERTION_HEADER = "X-Underleaf-Assertion"
CONNECTION_HEADER = "X-Underleaf-Connection"
CLUSTER_HEADER = "X-Underleaf-Cluster"

# Whatever the cluster-id alphabet is, it is not path syntax or whitespace.
_CLUSTER_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")
_BEARER = re.compile(r"^Bearer ([^\s]+)$", re.IGNORECASE)


def _empty(status_code: int, **headers: str) -> Response:
    return Response(status_code=status_code, headers=headers or None)


def _bearer(request: Request) -> str | None:
    values = request.headers.getlist("authorization")
    if len(values) != 1:  # none, or ambiguous
        return None
    m = _BEARER.match(values[0].strip())
    return m.group(1) if m else None


@router.get(
    "/remote-auth",
    summary="auth_request target: authenticate a remote-access caller and "
    "mint the per-request assertion",
    responses={
        200: {"description": "Authorized; assertion and connection in headers"},
        400: {"description": "Request carried X-Subject-Id"},
        401: {"description": "Invalid credential"},
        403: {"description": "Cluster missing or not the caller's"},
    },
)
async def remote_auth(
    request: Request,
    svc: RemoteAuthService = Depends(get_remote_auth_service),
) -> Response:
    # The X-Subject-Id bypass (nginx injects it for mTLS clients, and the
    # middleware turns it into a bearer token) must never authenticate a
    # remote caller, however it got here. Refuse before looking at anything.
    if "x-subject-id" in request.headers:
        logger.warning("remote-auth refused: request carried X-Subject-Id")
        return _empty(status.HTTP_400_BAD_REQUEST)

    credential = _bearer(request)
    account_id = await svc.authenticate(credential) if credential else None
    if account_id is None:
        return _empty(status.HTTP_401_UNAUTHORIZED, **{"WWW-Authenticate": "Bearer"})

    clusters = request.headers.getlist(CLUSTER_HEADER)
    cluster_id = clusters[0] if len(clusters) == 1 else ""
    if not _CLUSTER_ID.match(cluster_id):
        return _empty(status.HTTP_403_FORBIDDEN)

    try:
        grant = await svc.authorize(account_id, cluster_id)
    except ClusterAccessDeniedError:
        # Server-side only; the caller cannot tell missing from foreign.
        logger.bind(account_id=account_id, cluster_id=cluster_id).warning(
            "remote-auth denied: cluster missing or not owned"
        )
        return _empty(status.HTTP_403_FORBIDDEN)

    return _empty(
        status.HTTP_200_OK,
        **{
            ASSERTION_HEADER: grant.assertion,
            CONNECTION_HEADER: grant.connection_id,
            "Cache-Control": "no-store",
        },
    )
