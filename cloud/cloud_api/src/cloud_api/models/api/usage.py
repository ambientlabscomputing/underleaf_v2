from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, Field

from cloud_api.models.api.subscriptions import SubscriptionTier
from cloud_api.models.base import Base, IDPrefix, generate_id


class UsageEventType(StrEnum):
    DATA_USAGE = "data_usage"
    CONNECTION_SLOT = "connection_slot"


class UsageUnits(StrEnum):
    BYTES = "bytes"
    CONNECTION_SLOTS = "connection_slots"


class UsageEvent(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.USAGE_EVENT),
        description="Unique identifier for the usage event",
    )
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    event_type: UsageEventType = Field(..., description="Type of the usage event")
    usage_unit: UsageUnits = Field(..., description="Units of usage for the event")
    usage_amount: int = Field(..., description="Amount of usage for the event")
    stream_id: str | None = Field(
        default=None, description="Source stream, when derived from stream activity"
    )
    period_start: datetime = Field(
        ..., description="Start of the window this event's usage_amount covers"
    )
    period_end: datetime = Field(
        ..., description="End of the window this event's usage_amount covers"
    )
    idempotency_key: str = Field(
        ..., description="Caller-supplied dedupe key; unique across all usage events"
    )
    billing_period_id: str | None = Field(
        default=None,
        description="Billing period this event has been rolled up into, if any",
    )


class CreateUsageEventRequest(BaseModel):
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    event_type: UsageEventType = Field(..., description="Type of the usage event")
    usage_unit: UsageUnits = Field(..., description="Units of usage for the event")
    usage_amount: int = Field(..., description="Amount of usage for the event")
    stream_id: str | None = Field(
        default=None, description="Source stream, when derived from stream activity"
    )
    period_start: datetime = Field(
        ..., description="Start of the window this event's usage_amount covers"
    )
    period_end: datetime = Field(
        ..., description="End of the window this event's usage_amount covers"
    )
    idempotency_key: str = Field(
        ..., description="Caller-supplied dedupe key; unique across all usage events"
    )


class QueryUsageEventRequest(BaseModel):
    billing_account_id: str | None = Field(
        None, description="ID of the associated billing account"
    )
    event_type: UsageEventType | None = Field(
        None, description="Type of the usage event"
    )
    usage_unit: UsageUnits | None = Field(
        None, description="Units of usage for the event"
    )
    stream_id: str | None = Field(None, description="Source stream ID")


class ListUsageEventResponse(BaseModel):
    items: list[UsageEvent] = Field(..., description="List of usage events")
    total: int = Field(..., description="Total number of matching usage events")


class UsageAlertKind(StrEnum):
    USAGE_80 = "usage_80"
    USAGE_95 = "usage_95"
    USAGE_100 = "usage_100"
    TOP_UP_CAP_REACHED = "top_up_cap_reached"
    TOP_UP_FAILED = "top_up_failed"


class UsageAlert(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.ALERT),
        description="Unique identifier for the alert",
    )
    billing_account_id: str = Field(..., description="Associated billing account")
    kind: UsageAlertKind = Field(..., description="What happened")
    period_start: datetime = Field(..., description="Allowance period it belongs to")


class UsageSummary(BaseModel):
    billing_account_id: str
    tier: SubscriptionTier
    paid: bool = Field(..., description="On a Stripe-managed paid plan")
    period_start: datetime | None
    period_end: datetime | None
    allowance_bytes: int
    used_bytes: int
    remaining_bytes: int
    percent_used: float
    grace_policy_bytes: int
    grace_remaining_bytes: int
    grace_active: bool = Field(
        ..., description="Allowance exhausted and running on the grace allowance"
    )
    blocked: bool = Field(
        ..., description="Gateway-relayed traffic is currently stopped"
    )
    connection_slot_limit: int
    connection_slots_remaining: int
    auto_top_up_enabled: bool
    monthly_top_up_cap_cents: int
    top_up_spent_cents: int
    top_up_block_bytes: int
    top_up_price_cents: int
    can_top_up: bool
    alerts: list[UsageAlert]
