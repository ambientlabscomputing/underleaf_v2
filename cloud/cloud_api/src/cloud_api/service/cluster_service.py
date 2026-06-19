# ClusterService
#   RegisterCluster(CreateClusterRequest, principal_account_id) returns (Cluster);
#   GetClusters(QueryClusterRequest) returns (ListlusterResponse);
#   GetCluster(cluster_id) returns (Cluster);
#   PatchCluster(cluster_id, PatchClusterRequest) returns (Cluster);
#   SyncNodes(cluster_id, Nodes) returns (Cluster);
#   DeleteCluster(cluster_id) returns (Empty);

from cloud_api.models.api import (
    Cluster,
    CreateClusterRequest,
    CreateNodeRequest,
    ListClusterResponse,
    Node,
    PatchClusterRequest,
    QueryClusterRequest,
    QueryNodeRequest,
)
from cloud_api.repository.cluster_repository import ClusterRepository
from cloud_api.repository.node_repository import NodeRepository
from cloud_api import logger


class ClusterNotFoundError(Exception):
    def __init__(self, cluster_id: str) -> None:
        super().__init__(f"Cluster '{cluster_id}' not found")
        self.cluster_id = cluster_id


class ClusterService:
    def __init__(
        self,
        cluster_repo: ClusterRepository,
        node_repo: NodeRepository,
    ) -> None:
        self.cluster_repo = cluster_repo
        self.node_repo = node_repo

    async def register_cluster(
        self, req: CreateClusterRequest, principal_account_id: str
    ) -> Cluster:
        logger.bind(cluster_id=req.id, principal_account_id=principal_account_id).info(
            "Registering cluster"
        )
        cluster = await self.cluster_repo.create_cluster(
            req.model_copy(update={"principal_account_id": principal_account_id})
        )
        logger.bind(cluster_id=cluster.id).info("Cluster registered")
        return cluster

    async def get_clusters(self, req: QueryClusterRequest) -> ListClusterResponse:
        items = await self.cluster_repo.query_clusters(req)
        return ListClusterResponse(items=items, total=len(items))

    async def get_cluster(self, cluster_id: str) -> Cluster:
        cluster = await self.cluster_repo.get_cluster(cluster_id)
        if cluster is None:
            raise ClusterNotFoundError(cluster_id)
        return cluster

    async def patch_cluster(
        self, cluster_id: str, req: PatchClusterRequest
    ) -> Cluster:
        cluster = await self.cluster_repo.patch_cluster(cluster_id, req)
        if cluster is None:
            raise ClusterNotFoundError(cluster_id)
        return cluster

    async def sync_nodes(self, cluster_id: str, nodes: list[Node]) -> Cluster:
        """Replace the cluster's node set with the provided list.

        Nodes present in *nodes* but not in the DB are created.
        Nodes in the DB but absent from *nodes* are deleted.
        Nodes present in both are left unchanged.
        """
        cluster = await self.cluster_repo.get_cluster(cluster_id)
        if cluster is None:
            raise ClusterNotFoundError(cluster_id)

        current_nodes = await self.node_repo.query_nodes(
            QueryNodeRequest(name=None, cluster_id=cluster_id)
        )
        current_ids = {n.id for n in current_nodes}
        incoming_ids = {n.id for n in nodes}

        for node_id in current_ids - incoming_ids:
            await self.node_repo.delete_node(node_id)

        for node in nodes:
            if node.id not in current_ids:
                await self.node_repo.create_node(
                    CreateNodeRequest(id=node.id, name=node.name, cluster_id=cluster_id)
                )

        logger.bind(
            cluster_id=cluster_id,
            added=len(incoming_ids - current_ids),
            removed=len(current_ids - incoming_ids),
        ).info("Nodes synced")
        return cluster

    async def delete_cluster(self, cluster_id: str) -> None:
        deleted = await self.cluster_repo.delete_cluster(cluster_id)
        if not deleted:
            raise ClusterNotFoundError(cluster_id)
        logger.bind(cluster_id=cluster_id).info("Cluster deleted")
