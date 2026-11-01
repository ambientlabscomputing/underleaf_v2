from datetime import UTC, datetime
from enum import StrEnum

from pydantic import BaseModel, Field

from cloud_api.models.api.common import Status
from cloud_api.models.base import Base


class WorkerState(StrEnum):
    """Node in the worker state machine. Paired with `Status`: `state` is the
    node the worker is in (or heading to), `status` says whether the
    transition into it has been reached (in_progress / succeeded / failed).

    REQUESTED -> CREATED -> CONFIGURED -> AVAILABLE -> DRAINING -> DELETED

    Every node is entered as `IN_PROGRESS` and settles to `SUCCEEDED` (or
    `FAILED`). So `DELETED + IN_PROGRESS` is a teardown under way (the VM may
    still exist and still costs money) and only `DELETED + SUCCEEDED` means
    the worker is gone.
    """

    REQUESTED = "REQUESTED"  # demand recorded, no VM yet
    CREATED = "CREATED"  # VM exists at the provider
    CONFIGURED = "CONFIGURED"  # bootstrap finished (ready callback received)
    AVAILABLE = "AVAILABLE"  # serving; AVAILABLE + SUCCEEDED == ready
    DRAINING = "DRAINING"  # out of service; IN_PROGRESS while the drain period runs
    DELETED = "DELETED"  # IN_PROGRESS: tearing down; SUCCEEDED: gone


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
    image: str = Field(
        default="",
        description="Container image the cloud-init bootstrap runs. The SSH "
        "bootstrap uses install_cmd/init_cmd instead.",
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
    state_changed_at: datetime | None = Field(
        default=None, description="When `state`/`status` last changed"
    )

    @property
    def is_ready(self) -> bool:
        return self.state == WorkerState.AVAILABLE and self.status == Status.SUCCEEDED

    @property
    def is_gone(self) -> bool:
        return self.state == WorkerState.DELETED and self.status == Status.SUCCEEDED

    @property
    def is_live(self) -> bool:
        """Still holds the per-type slot (and may still be costing money).
        Mirrors the partial unique index on `workers`."""
        return not self.is_gone


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


class WorkerTaskKind(StrEnum):
    CREATE_CONNECTION = "create_connection"


class WorkerTaskState(StrEnum):
    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"


class WorkerTask(Base):
    """A durable unit of work waiting on a worker (the queue row + audit log)."""

    kind: WorkerTaskKind
    state: WorkerTaskState = WorkerTaskState.QUEUED
    payload: dict = Field(default_factory=dict)
    connection_id: str | None = None
    worker_id: str | None = None
    attempts: int = 0
    max_attempts: int = 5
    run_after: datetime = Field(default_factory=lambda: datetime.now(UTC))
    lease_expires_at: datetime | None = None
    error: str | None = None
    started_at: datetime | None = None
    finished_at: datetime | None = None


class WorkerEvent(Base):
    """Append-only timeline entry for a worker/task/connection."""

    worker_id: str | None = None
    task_id: str | None = None
    connection_id: str | None = None
    type: str = Field(..., description="e.g. worker.transition, task.attempt")
    message: str = ""
    data: dict = Field(default_factory=dict)
