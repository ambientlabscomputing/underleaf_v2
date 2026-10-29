from datetime import UTC, datetime
from enum import StrEnum

from epyxid import XID
from pydantic import BaseModel, Field


class IDPrefix(StrEnum):
    PRINCIPAL_ACCOUNT = "principal"
    USER = "user"
    CREDENTIAL = "cred"
    ORG = "org"
    CONNECTION = "conn"
    STREAM = "stream"
    CLUSTER = "cluster"
    BILLING_ACCOUNT = "billing"
    ENTITLEMENTS_BUCKET = "entitlements"
    NODE = "node"
    USAGE_EVENT = "usage"
    CREDIT_EVENT = "credit"
    SUBSCRIPTION = "subscription"
    CANDIDATE = "candidate"
    PLAN = "plan"
    PLAN_PRICE = "price"
    INVOICE = "invoice"
    WEBHOOK_EVENT = "webhook"
    ALERT = "alert"
    TOP_UP = "topup"


def generate_id(prefix: IDPrefix) -> str:
    return f"{prefix.value}_{XID()!s}"


class Base(BaseModel):
    class Config:
        from_attributes = True

    id: str = Field(..., description="Unique identifier for the model instance")
    created_at: datetime = Field(
        default_factory=lambda: datetime.now(UTC),
        description="Timestamp when the instance was created",
    )
    updated_at: datetime = Field(
        default_factory=lambda: datetime.now(UTC),
        description="Timestamp when the instance was last updated",
    )
