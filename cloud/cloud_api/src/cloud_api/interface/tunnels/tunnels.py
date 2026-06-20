from fastapi import APIRouter, Depends, HTTPException, status

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    CreateTunnelRequest,
    ListTunnelResponse,
    PatchTunnelRequest,
    QueryTunnelRequest,
    Tunnel,
)
from cloud_api.service.tunnel_service import TunnelNotFoundError, TunnelService
from cloud_api.service_manager import get_tunnel_service

router = APIRouter(prefix="/tunnels", tags=["Tunnels"])


@router.post(
    "",
    response_model=Tunnel,
    status_code=status.HTTP_201_CREATED,
    summary="Create a tunnel",
)
async def create_tunnel(
    req: CreateTunnelRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    tunnel_service: TunnelService = Depends(get_tunnel_service),
) -> Tunnel:
    return await tunnel_service.create_tunnel(req)


@router.get(
    "",
    response_model=ListTunnelResponse,
    summary="List tunnels",
)
async def get_tunnels(
    name: str | None = None,
    node_id: str | None = None,
    claims: AccessTokenClaims = Depends(get_access_claims),
    tunnel_service: TunnelService = Depends(get_tunnel_service),
) -> ListTunnelResponse:
    return await tunnel_service.get_tunnels(
        QueryTunnelRequest(name=name, node_id=node_id, principal_account_id=claims.azp)
    )


@router.get(
    "/{tunnel_id}",
    response_model=Tunnel,
    summary="Get a tunnel by ID",
)
async def get_tunnel(
    tunnel_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    tunnel_service: TunnelService = Depends(get_tunnel_service),
) -> Tunnel:
    try:
        return await tunnel_service.get_tunnel(tunnel_id)
    except TunnelNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Tunnel not found")


@router.patch(
    "/{tunnel_id}",
    response_model=Tunnel,
    summary="Update a tunnel",
)
async def patch_tunnel(
    tunnel_id: str,
    req: PatchTunnelRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    tunnel_service: TunnelService = Depends(get_tunnel_service),
) -> Tunnel:
    try:
        return await tunnel_service.patch_tunnel(tunnel_id, req)
    except TunnelNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Tunnel not found")


@router.delete(
    "/{tunnel_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    summary="Delete a tunnel",
)
async def delete_tunnel(
    tunnel_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    tunnel_service: TunnelService = Depends(get_tunnel_service),
) -> None:
    try:
        await tunnel_service.delete_tunnel(tunnel_id)
    except TunnelNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Tunnel not found")
