from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, Field

from cloud_api.models.api.common import Status
from cloud_api.models.base import Base, IDPrefix, generate_id


class StreamType(StrEnum):
    SSH = "ssh"
    GW = "gateway"
    Tunnel = "tunnel"


class NewStreamRequest(BaseModel):
    connection_id: str = Field(..., description="ID of the associated connection")
    type_: StreamType = Field(..., description="Type of the stream", alias="type")
    endpoint: str | None = Field(None, description="Optional endpoint for the stream")
    port: int | None = Field(None, description="Optional port for the stream")


class CloseStreamRequest(BaseModel):
    stream_id: str = Field(..., description="ID of the stream to close")


class CloseStreamResponse(BaseModel):
    id: str = Field(..., description="ID of the closed stream")
    status: str = Field(..., description="Status of the close request")


class StreamState(StrEnum):
    ACTIVE = "active"
    CLOSED = "closed"


class Stream(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.STREAM),
        description="Unique identifier for the stream",
    )
    name: str = Field(..., description="Name of the stream")
    connection_id: str = Field(..., description="ID of the associated connection")
    type_: StreamType = Field(..., description="Type of the stream", alias="type")
    state: StreamState = Field(
        default=StreamState.ACTIVE, description="Current state of the stream"
    )
    status: Status = Field(
        default=Status.IN_PROGRESS, description="Current status of the stream"
    )
    endpoint: str | None = Field(
        default=None, description="Optional endpoint for the stream"
    )
    port: int | None = Field(default=None, description="Optional port for the stream")
    closed_at: datetime | None = Field(
        default=None, description="Timestamp when the stream was closed"
    )
    last_reported_bytes: int = Field(
        default=0,
        description=(
            "Cumulative bytes already turned into a usage_events row for "
            "this stream -- internal bookkeeping, not client-settable"
        ),
    )
    # Running totals as reported by conn_worker. Transient: not a DB column,
    # so excluded from model_dump (batch inserts) and API responses.
    bytes_in: int = Field(default=0, exclude=True)
    bytes_out: int = Field(default=0, exclude=True)


class PatchStreamRequest(BaseModel):
    name: str | None = Field(default=None, description="Name of the stream")
    state: StreamState | None = Field(
        default=None, description="Current state of the stream"
    )
    status: Status | None = Field(
        default=None, description="Current status of the stream"
    )
    closed_at: datetime | None = Field(
        default=None, description="Timestamp when the stream was closed"
    )


class QueryStreamRequest(BaseModel):
    name: str | None = Field(default=None, description="Name of the stream")
    connection_id: str | None = Field(
        default=None, description="ID of the associated connection"
    )
    principal_account_id: str | None = Field(
        default=None, description="Scope to a principal account"
    )
    node_id: str | None = Field(default=None, description="Scope to a node")
    state: StreamState | None = Field(default=None, description="State of the stream")
    status: Status | None = Field(default=None, description="Status of the stream")
    endpoint: str | None = Field(default=None, description="Endpoint of the stream")
    port: int | None = Field(default=None, description="Port of the stream")


class ListStreamResponse(BaseModel):
    items: list[Stream] = Field(..., description="List of streams")
    total: int = Field(..., description="Total number of matching streams")
