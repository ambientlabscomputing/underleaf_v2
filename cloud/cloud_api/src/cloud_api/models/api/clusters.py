from pydantic import BaseModel, Field

from cloud_api.models.base import Base


# Cluster and Node IDs don't get generators because they are tracked by the local clusters
# themselves and will come with a canonical ID.
class Cluster(Base):
    id: str = Field(
        ...,
        description="Unique identifier for the cluster",
    )
    name: str = Field(..., description="Name of the cluster")
    principal_account_id: str = Field(
        ..., description="ID of the associated principal account"
    )
    manager_node_id: str | None = Field(
        None, description="ID of the manager node for the cluster"
    )


class CreateClusterRequest(BaseModel):
    id: str = Field(..., description="Unique identifier for the cluster")
    name: str = Field(..., description="Name of the cluster")
    principal_account_id: str | None = Field(
        None,
        description="Principal account ID — injected server-side, not required in request body",
    )


class PatchClusterRequest(BaseModel):
    name: str | None = Field(None, description="Name of the cluster")


class QueryClusterRequest(BaseModel):
    name: str | None = Field(None, description="Name of the cluster")
    principal_account_id: str | None = Field(
        None, description="ID of the associated principal account"
    )


class ListClusterResponse(BaseModel):
    items: list["Cluster"] = Field(..., description="List of clusters")
    total: int = Field(..., description="Total number of matching clusters")


class Node(Base):
    id: str = Field(
        ...,
        description="Unique identifier for the node",
    )
    name: str = Field(..., description="Name of the node")
    cluster_id: str = Field(..., description="ID of the associated cluster")


class CreateNodeRequest(BaseModel):
    id: str = Field(..., description="Unique identifier for the node")
    name: str = Field(..., description="Name of the node")
    cluster_id: str = Field(..., description="ID of the associated cluster")


class PatchNodeRequest(BaseModel):
    name: str | None = Field(None, description="Name of the node")


class QueryNodeRequest(BaseModel):
    name: str | None = Field(None, description="Name of the node")
    cluster_id: str | None = Field(None, description="ID of the associated cluster")
