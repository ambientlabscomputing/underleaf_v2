from datetime import datetime

from pydantic import BaseModel, Field

from cloud_api.models.base import Base, IDPrefix, generate_id


class EntitlementsBucket(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.ENTITLEMENTS_BUCKET),
        description="Unique identifier for the entitlements bucket",
    )
    name: str = Field(..., description="Name of the entitlements bucket")
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )

    network_traffic_balance: int = Field(
        default=0,
        description="Available balance for network traffic in the entitlements bucket in bytes",
    )
    connection_slot_balance: int = Field(
        default=0,
        description="Available balance for connection slots in the entitlements bucket",
    )
    network_traffic_allowance: int = Field(
        default=0,
        description="Bytes available this period (included + purchased top-ups); the denominator for usage %",
    )
    connection_slot_limit: int = Field(
        default=0, description="Max concurrently open connections"
    )
    grace_policy_bytes: int = Field(
        default=0,
        description="Temporary grace allowance granted once when the allowance is exhausted (0 = hard stop)",
    )
    grace_balance: int = Field(default=0, description="Grace bytes remaining")
    grace_granted_at: datetime | None = Field(
        default=None, description="When the current grace allowance was granted"
    )
    period_start: datetime | None = Field(
        default=None, description="Start of the current allowance period"
    )
    period_end: datetime | None = Field(
        default=None, description="End of the current allowance period"
    )
    stripe_managed_period: bool = Field(
        default=False,
        description="True when renewals come from Stripe (paid); False = calendar-month free tier",
    )


class CreateEntitlementsBucketRequest(BaseModel):
    name: str = Field(..., description="Name of the entitlements bucket")
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    network_traffic_allowance: int = Field(default=0)
    connection_slot_limit: int = Field(default=0)
    grace_policy_bytes: int = Field(default=0)
    period_start: datetime | None = Field(default=None)
    period_end: datetime | None = Field(default=None)
    stripe_managed_period: bool = Field(default=False)


class PatchEntitlementsBucketRequest(BaseModel):
    name: str | None = Field(None, description="Name of the entitlements bucket")
    network_traffic_balance: int | None = Field(
        None, description="Available balance for network traffic in bytes"
    )
    connection_slot_balance: int | None = Field(
        None, description="Available balance for connection slots"
    )


class QueryEntitlementsBucketRequest(BaseModel):
    name: str | None = Field(None, description="Name of the entitlements bucket")
    billing_account_id: str | None = Field(
        None, description="ID of the associated billing account"
    )


class ListEntitlementsBucketResponse(BaseModel):
    items: list[EntitlementsBucket] = Field(
        ..., description="List of entitlements buckets"
    )
    total: int = Field(..., description="Total number of matching entitlements buckets")


class EntitlementsBalance(BaseModel):
    id: str = Field(..., description="ID of the entitlements bucket")
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    network_traffic_balance: int = Field(
        ..., description="Remaining network traffic balance in bytes"
    )
    connection_slot_balance: int = Field(
        ..., description="Remaining connection slot balance"
    )
