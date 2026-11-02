from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, Field

from cloud_api.models.api.common import Status
from cloud_api.models.base import Base, IDPrefix, generate_id


class CreateConnectionRequest(BaseModel):
    name: str = Field(..., description="Name of the connection")
    node_id: str = Field(..., description="ID of the associated node")


class TerminateConnRequest(BaseModel):
    connection_id: str = Field(..., description="ID of the connection to terminate")


class TerminateConnResponse(BaseModel):
    id: str = Field(..., description="ID of the terminated connection")
    status: str = Field(..., description="Status of the termination request")


class ConnectionState(StrEnum):
    PROVISIONED = "provisioned"
    RUNNING = "running"
    CLOSED = "closed"


class Connection(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.CONNECTION),
        description="Unique identifier for the connection",
    )
    name: str = Field(..., description="Name of the connection")
    node_id: str = Field(..., description="ID of the associated node")
    closed_at: datetime | None = Field(
        default=None, description="Timestamp when the connection was closed"
    )
    state: ConnectionState = Field(
        default=ConnectionState.PROVISIONED,
        description="Current state of the connection",
    )
    status: Status = Field(
        default=Status.IN_PROGRESS, description="Current status of the connection"
    )


class PatchConnectionRequest(BaseModel):
    name: str | None = Field(default=None, description="Name of the connection")
    closed_at: datetime | None = Field(
        default=None, description="Timestamp when the connection was closed"
    )
    state: ConnectionState | None = Field(
        default=None, description="Current state of the connection"
    )
    status: Status | None = Field(
        default=None, description="Current status of the connection"
    )


class QueryConnectionRequest(BaseModel):
    name: str | None = Field(None, description="Name of the connection")
    node_id: str | None = Field(None, description="ID of the associated node")
    principal_account_id: str | None = Field(
        None, description="Scope to a principal account"
    )
    state: ConnectionState | None = Field(None, description="State of the connection")
    status: Status | None = Field(None, description="Status of the connection")


class ListConnectionResponse(BaseModel):
    items: list[Connection] = Field(..., description="List of connections")
    total: int = Field(..., description="Total number of matching connections")
