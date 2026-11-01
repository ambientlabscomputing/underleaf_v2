from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, Field


class VMProvider(StrEnum):
    DIGITAL_OCEAN = "digital_ocean"
    FAKE = "fake"  # in-memory; tests and local dev without cloud spend


class VMStatus(StrEnum):
    """Provider-neutral lifecycle; each adapter maps its own states onto this."""

    PENDING = "pending"  # requested, not yet running
    ACTIVE = "active"  # running and has an IP
    DELETING = "deleting"
    DELETED = "deleted"
    ERROR = "error"


class VMSpec(BaseModel):
    cpus: int
    mem_gb: int
    storage_gb: int
    region: str | None = (
        None  # provider-specific hint; adapter falls back to its default
    )


class VM(BaseModel):
    spec: VMSpec
    provider_id: str = Field(..., description="The provider's own handle for the VM")
    hostname: str
    os: str
    status: VMStatus
    provider: VMProvider
    public_ip: str | None = None
    private_ip: str | None = None
    tags: dict[str, str] = Field(default_factory=dict)
    created_at: datetime | None = None
