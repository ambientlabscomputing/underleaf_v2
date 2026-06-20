from cloud_api.models.api import (
    Tunnel,
    CreateTunnelRequest,
    ListTunnelResponse,
    PatchTunnelRequest,
    QueryTunnelRequest,
)
from cloud_api.repository.tunnel_repository import TunnelRepository
from cloud_api import logger


class TunnelNotFoundError(Exception):
    def __init__(self, tunnel_id: str) -> None:
        super().__init__(f"Tunnel '{tunnel_id}' not found")
        self.tunnel_id = tunnel_id


class TunnelService:
    def __init__(self, tunnel_repo: TunnelRepository) -> None:
        self.tunnel_repo = tunnel_repo

    async def create_tunnel(self, req: CreateTunnelRequest) -> Tunnel:
        tunnel = await self.tunnel_repo.create_tunnel(req)
        logger.bind(tunnel_id=tunnel.id).info("Tunnel created")
        return tunnel

    async def get_tunnels(self, req: QueryTunnelRequest) -> ListTunnelResponse:
        items = await self.tunnel_repo.query_tunnels(req)
        return ListTunnelResponse(items=items, total=len(items))

    async def get_tunnel(self, tunnel_id: str) -> Tunnel:
        tunnel = await self.tunnel_repo.get_tunnel(tunnel_id)
        if tunnel is None:
            raise TunnelNotFoundError(tunnel_id)
        return tunnel

    async def patch_tunnel(self, tunnel_id: str, req: PatchTunnelRequest) -> Tunnel:
        tunnel = await self.tunnel_repo.patch_tunnel(tunnel_id, req)
        if tunnel is None:
            raise TunnelNotFoundError(tunnel_id)
        return tunnel

    async def delete_tunnel(self, tunnel_id: str) -> None:
        deleted = await self.tunnel_repo.delete_tunnel(tunnel_id)
        if not deleted:
            raise TunnelNotFoundError(tunnel_id)
        logger.bind(tunnel_id=tunnel_id).info("Tunnel deleted")
