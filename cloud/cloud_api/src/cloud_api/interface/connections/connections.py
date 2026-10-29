from typing import Annotated

from fastapi import APIRouter, Depends, HTTPException, Query, status

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    Connection,
    CreateConnectionRequest,
    ListConnectionResponse,
    PatchConnectionRequest,
    QueryConnectionRequest,
)
from cloud_api.service.connection_service import (
    ConnectionNotFoundError,
    ConnectionService,
)
from cloud_api.service.entitlements_bucket_service import EntitlementsExceededError
from cloud_api.service_manager import get_connection_service

router = APIRouter(prefix="/connections", tags=["Connections"])


def _not_found() -> HTTPException:
    return HTTPException(
        status_code=status.HTTP_404_NOT_FOUND, detail="Connection not found"
    )


async def _require_owned(
    connection_id: str, claims: AccessTokenClaims, service: ConnectionService
) -> None:
    """404 unless the caller's tenant owns the connection. A foreign
    connection is reported as not-found rather than forbidden so IDs can't be
    probed."""
    if await service.get_owner_principal_id(connection_id) != claims.azp:
        raise _not_found()


@router.post(
    "",
    response_model=Connection,
    status_code=status.HTTP_201_CREATED,
    summary="Create a connection",
)
async def create_connection(
    req: CreateConnectionRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    connection_service: ConnectionService = Depends(get_connection_service),
) -> Connection:
    # The billing account charged for the slot is resolved from the node, so
    # the node must belong to the caller.
    if await connection_service.get_node_owner_principal_id(req.node_id) != claims.azp:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Node not found"
        )
    try:
        return await connection_service.create_connection(req)
    except EntitlementsExceededError:
        raise HTTPException(
            status_code=status.HTTP_402_PAYMENT_REQUIRED,
            detail="Connection limit reached for your plan.",
        )


@router.get(
    "",
    response_model=ListConnectionResponse,
    summary="List connections",
)
async def get_connections(
    query: Annotated[QueryConnectionRequest, Query()],
    claims: AccessTokenClaims = Depends(get_access_claims),
    connection_service: ConnectionService = Depends(get_connection_service),
) -> ListConnectionResponse:
    # Scope to the caller's tenant; previously every authenticated user could
    # list every connection.
    query.principal_account_id = claims.azp
    return await connection_service.get_connections(query)


@router.get(
    "/{connection_id}",
    response_model=Connection,
    summary="Get a connection by ID",
)
async def get_connection(
    connection_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    connection_service: ConnectionService = Depends(get_connection_service),
) -> Connection:
    await _require_owned(connection_id, claims, connection_service)
    try:
        return await connection_service.get_connection(connection_id)
    except ConnectionNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Connection not found"
        )


@router.patch(
    "/{connection_id}",
    response_model=Connection,
    summary="Update a connection",
)
async def patch_connection(
    connection_id: str,
    req: PatchConnectionRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    connection_service: ConnectionService = Depends(get_connection_service),
) -> Connection:
    await _require_owned(connection_id, claims, connection_service)
    try:
        return await connection_service.patch_connection(connection_id, req)
    except ConnectionNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Connection not found"
        )


@router.post(
    "/{connection_id}/terminate",
    response_model=Connection,
    summary="Terminate a connection",
)
async def terminate_connection(
    connection_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    connection_service: ConnectionService = Depends(get_connection_service),
) -> Connection:
    await _require_owned(connection_id, claims, connection_service)
    try:
        return await connection_service.terminate_connection(connection_id)
    except ConnectionNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Connection not found"
        )


@router.delete(
    "/{connection_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    summary="Delete a connection",
)
async def delete_connection(
    connection_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    connection_service: ConnectionService = Depends(get_connection_service),
) -> None:
    await _require_owned(connection_id, claims, connection_service)
    try:
        await connection_service.delete_connection(connection_id)
    except ConnectionNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Connection not found"
        )
