from pydantic import BaseModel, Field
from epyxid import XID
from enum import StrEnum
from datetime import datetime, timezone


class IDPrefix(StrEnum):
    PRINCIPAL_ACCOUNT = "principal"
    USER = "user"
    CREDENTIAL = "cred"
    ORG = "org"
    TUNNEL = "tunnel"
    CONNECTION = "conn"
    CLUSTER = "cluster"
    BILLING_ACCOUNT = "billing"
    ENTITLEMENTS_BUCKET = "entitlements"
    NODE = "node"
    USAGE_EVENT = "usage"
    CREDIT_EVENT = "credit"
    SUBSCRIPTION = "subscription"
    CANDIDATE = "candidate"


def generate_id(prefix: IDPrefix) -> str:
    return f"{prefix.value}_{str(XID())}"


class Base(BaseModel):
    class Config:
        from_attributes = True

    id: str = Field(..., description="Unique identifier for the model instance")
    created_at: datetime = Field(
        default_factory=lambda: datetime.now(timezone.utc),
        description="Timestamp when the instance was created",
    )
    updated_at: datetime = Field(
        default_factory=lambda: datetime.now(timezone.utc),
        description="Timestamp when the instance was last updated",
    )
