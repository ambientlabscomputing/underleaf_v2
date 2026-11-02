from enum import StrEnum

from pydantic import BaseModel, Field

from cloud_api.models.api.common import Status
from cloud_api.models.base import Base


class WorkerState(StrEnum):
    REQUESTED = "REQUESTED"
    CREATED = "CREATED"
    AVAILABLE = "AVAILABLE"
    DELETING = "DELETING"
    DELETED = "DELETED"


class WorkerType(StrEnum):
    CONNECTION_WORKER = "connection_worker"


class WorkerConfig(BaseModel):
    cpu: int = Field(..., description="Number of CPU cores")
    mem_gb: int = Field(..., description="Amount of memory in GB")
    storage_gb: int = Field(..., description="Amount of storage in GB")
    repository: str = Field(..., description="Repository URL")
    install_cmd: list[str] = Field(..., description="Installation command")
    init_cmd: list[str] = Field(..., description="Initialization command")
    init_cmd_cwd: str = Field(
        default=".", description="Working directory for the initialization command"
    )
    install_cmd_cwd: str = Field(
        default=".", description="Working directory for the installation command"
    )
    repository_host: str = Field(
        default="https://github.com/", description="Repository host URL"
    )


class Worker(Base):
    id: str = Field(..., description="Unique identifier for the worker")
    worker_type: WorkerType = Field(..., description="Type of cluster")
    config: WorkerConfig = Field(..., description="Configuration for the worker")
    state: WorkerState = Field(..., description="Current state of the worker")
    status: Status = Field(..., description="Current status of the worker")
    extra_data: dict = Field(
        default_factory=dict, description="Additional data for the worker"
    )
    error: str | None = None


class QueryWorkerRequest(BaseModel):
    class Config:
        extra = "allow"

    offset: int = Field(default=0, description="Offset for pagination")
    limit: int = Field(default=10, description="Limit for pagination")
    order_by: str = Field(
        default="updated_at", description="Field to order the results by"
    )
    order_direction: str = Field(
        default="asc", description="Direction to order the results by (asc or desc)"
    )

    state: WorkerState | None = Field(None, description="Current state of the worker")
    status: Status | None = Field(None, description="Current status of the worker")
    worker_type: WorkerType | None = Field(None, description="Worker type")
