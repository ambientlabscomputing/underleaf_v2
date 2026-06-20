from cloud_api.models.api import (
    Tunnel,
    CreateTunnelRequest,
    QueryTunnelRequest,
    PatchTunnelRequest,
)
from cloud_api.models.sql import SqlTunnel, SQLCluster, SQLNode
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update
from datetime import datetime, timezone


class TunnelRepository(BaseRepository):
    async def create_tunnel(self, req: CreateTunnelRequest) -> Tunnel:
        async with self.get_session() as session:
            new_tunnel = SqlTunnel(
                name=req.name,
                node_id=req.node_id,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
            )
            session.add(new_tunnel)
            await session.commit()
            await session.refresh(new_tunnel)
            return Tunnel.model_validate(new_tunnel)

    async def get_tunnel(self, tunnel_id: str) -> Tunnel | None:
        async with self.get_session() as session:
            tunnel = await session.get(SqlTunnel, tunnel_id)
            if tunnel is None:
                return None
            return Tunnel.model_validate(tunnel)

    async def query_tunnels(self, req: QueryTunnelRequest) -> list[Tunnel]:
        async with self.get_session() as session:
            query = (
                select(SqlTunnel)
                .join(SQLNode, SqlTunnel.node_id == SQLNode.id)
                .join(SQLCluster, SQLNode.cluster_id == SQLCluster.id)
            )
            if req.name is not None:
                query = query.filter(SqlTunnel.name == req.name)
            if req.node_id is not None:
                query = query.filter(SqlTunnel.node_id == req.node_id)
            if req.principal_account_id is not None:
                query = query.filter(SQLCluster.principal_account_id == req.principal_account_id)
            result = await session.scalars(query)
            tunnels = result.all()
            return [Tunnel.model_validate(tunnel) for tunnel in tunnels]

    async def patch_tunnel(
        self, tunnel_id: str, req: PatchTunnelRequest
    ) -> Tunnel | None:
        async with self.get_session() as session:
            update_stmt = update(SqlTunnel).where(SqlTunnel.id == tunnel_id).values(updated_at=datetime.now(timezone.utc))
            if req.name is not None:
                update_stmt = update_stmt.values(name=req.name)
            result = await session.execute(update_stmt)
            if result.scalar() is None:
                return None
            await session.commit()
            updated_tunnel = await session.get(SqlTunnel, tunnel_id)
            return Tunnel.model_validate(updated_tunnel)

    async def delete_tunnel(self, tunnel_id: str) -> bool:
        async with self.get_session() as session:
            tunnel = await session.get(SqlTunnel, tunnel_id)
            if tunnel is None:
                return False
            await session.delete(tunnel)
            await session.commit()
            return True
