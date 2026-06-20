from fastapi import APIRouter, Depends, HTTPException, status

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    Connection,
    CreateConnectionRequest,
    ListConnectionResponse,
    PatchConnectionRequest,
    QueryConnectionRequest,
)
from cloud_api.service.connection_service import ConnectionNotFoundError, ConnectionService
from cloud_api.service_manager import get_connection_service

router = APIRouter(prefix="/connections", tags=["Connections"])


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
    return await connection_service.create_connection(req)


@router.get(
    "",
    response_model=ListConnectionResponse,
    summary="List connections",
)
async def get_connections(
    name: str | None = None,
    tunnel_id: str | None = None,
    claims: AccessTokenClaims = Depends(get_access_claims),
    connection_service: ConnectionService = Depends(get_connection_service),
) -> ListConnectionResponse:
    return await connection_service.get_connections(
        QueryConnectionRequest(name=name, tunnel_id=tunnel_id, principal_account_id=claims.azp)
    )


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
    try:
        return await connection_service.get_connection(connection_id)
    except ConnectionNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Connection not found")


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
    try:
        return await connection_service.patch_connection(connection_id, req)
    except ConnectionNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Connection not found")


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
    try:
        await connection_service.delete_connection(connection_id)
    except ConnectionNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Connection not found")
