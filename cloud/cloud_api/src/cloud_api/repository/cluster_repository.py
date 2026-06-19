from cloud_api.models.api import (
    Cluster,
    CreateClusterRequest,
    QueryClusterRequest,
    PatchClusterRequest,
)
from cloud_api.models.sql import SQLCluster
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update
from datetime import datetime, timezone


class ClusterRepository(BaseRepository):
    async def create_cluster(self, req: CreateClusterRequest) -> Cluster:
        async with self.get_session() as session:
            new_cluster = SQLCluster(
                id=req.id,
                name=req.name,
                principal_account_id=req.principal_account_id,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
            )
            session.add(new_cluster)
            await session.commit()
            await session.refresh(new_cluster)
            return Cluster.model_validate(new_cluster)

    async def get_cluster(self, cluster_id: str) -> Cluster | None:
        async with self.get_session() as session:
            cluster = await session.get(SQLCluster, cluster_id)
            if cluster is None:
                return None
            return Cluster.model_validate(cluster)

    async def query_clusters(self, req: QueryClusterRequest) -> list[Cluster]:
        async with self.get_session() as session:
            query = select(SQLCluster)
            if req.name is not None:
                query = query.filter(SQLCluster.name == req.name)
            if req.principal_account_id is not None:
                query = query.filter(
                    SQLCluster.principal_account_id == req.principal_account_id
                )
            result = await session.scalars(query)
            clusters = result.all()
            return [Cluster.model_validate(cluster) for cluster in clusters]

    async def patch_cluster(
        self, cluster_id: str, req: PatchClusterRequest
    ) -> Cluster | None:
        async with self.get_session() as session:
            update_stmt = update(SQLCluster).where(SQLCluster.id == cluster_id).values(updated_at=datetime.now(timezone.utc))
            if req.name is not None:
                update_stmt = update_stmt.values(name=req.name)
            result = await session.execute(update_stmt)
            if result.scalar() is None:
                return None
            await session.commit()
            updated_cluster = await session.get(SQLCluster, cluster_id)
            return Cluster.model_validate(updated_cluster)

    async def delete_cluster(self, cluster_id: str) -> bool:
        async with self.get_session() as session:
            cluster = await session.get(SQLCluster, cluster_id)
            if cluster is None:
                return False
            await session.delete(cluster)
            await session.commit()
            return True
