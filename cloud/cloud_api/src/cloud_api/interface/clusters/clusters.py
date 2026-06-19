from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    Cluster,
    CreateClusterRequest,
    ListClusterResponse,
    Node,
    PatchClusterRequest,
    QueryClusterRequest,
)
from cloud_api.service.cluster_service import ClusterNotFoundError, ClusterService
from cloud_api.service_manager import get_cluster_service

router = APIRouter(prefix="/clusters", tags=["Clusters"])


class SyncNodesRequest(BaseModel):
    nodes: list[Node]


@router.post(
    "",
    response_model=Cluster,
    status_code=status.HTTP_201_CREATED,
    summary="Register a new cluster",
)
async def register_cluster(
    req: CreateClusterRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    cluster_service: ClusterService = Depends(get_cluster_service),
) -> Cluster:
    return await cluster_service.register_cluster(req, claims.azp)


@router.get(
    "",
    response_model=ListClusterResponse,
    summary="List clusters for the authenticated principal account",
)
async def get_clusters(
    name: str | None = None,
    claims: AccessTokenClaims = Depends(get_access_claims),
    cluster_service: ClusterService = Depends(get_cluster_service),
) -> ListClusterResponse:
    return await cluster_service.get_clusters(
        QueryClusterRequest(name=name, principal_account_id=claims.azp)
    )


@router.get(
    "/{cluster_id}",
    response_model=Cluster,
    summary="Get a cluster by ID",
)
async def get_cluster(
    cluster_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    cluster_service: ClusterService = Depends(get_cluster_service),
) -> Cluster:
    try:
        return await cluster_service.get_cluster(cluster_id)
    except ClusterNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Cluster not found")


@router.patch(
    "/{cluster_id}",
    response_model=Cluster,
    summary="Update a cluster",
)
async def patch_cluster(
    cluster_id: str,
    req: PatchClusterRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    cluster_service: ClusterService = Depends(get_cluster_service),
) -> Cluster:
    try:
        return await cluster_service.patch_cluster(cluster_id, req)
    except ClusterNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Cluster not found")


@router.post(
    "/{cluster_id}/nodes/sync",
    response_model=Cluster,
    summary="Sync the node set for a cluster (replace-reconcile)",
)
async def sync_nodes(
    cluster_id: str,
    req: SyncNodesRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    cluster_service: ClusterService = Depends(get_cluster_service),
) -> Cluster:
    try:
        return await cluster_service.sync_nodes(cluster_id, req.nodes)
    except ClusterNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Cluster not found")


@router.delete(
    "/{cluster_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    summary="Delete a cluster",
)
async def delete_cluster(
    cluster_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    cluster_service: ClusterService = Depends(get_cluster_service),
) -> None:
    try:
        await cluster_service.delete_cluster(cluster_id)
    except ClusterNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Cluster not found")