from pydantic import BaseModel, Field

from cloud_api.models.base import Base, IDPrefix, generate_id


class BillingAccount(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.BILLING_ACCOUNT),
        description="Unique identifier for the billing account",
    )
    name: str = Field(..., description="Name of the billing account")
    principal_account_id: str = Field(
        ..., description="ID of the associated principal account"
    )
    stripe_customer_id: str | None = Field(
        default=None,
        description="Stripe customer ID associated with the billing account",
    )
    stripe_data: dict = Field(
        default={}, description="Stripe-related data for the billing account"
    )
    auto_top_up_enabled: bool = Field(
        default=False,
        description="Automatically buy a prepaid data block when the allowance runs out",
    )
    monthly_top_up_cap_cents: int = Field(
        default=5000,
        description="Max automatic top-up spend per billing period, in cents",
    )


class CreateBillingAccountRequest(BaseModel):
    name: str = Field(..., description="Name of the billing account")
    principal_account_id: str = Field(
        ..., description="ID of the associated principal account"
    )


class PatchBillingAccountRequest(BaseModel):
    """Customer-facing PATCH. Deliberately excludes Stripe identifiers: a
    customer-settable stripe_customer_id would let anyone point their account
    at someone else's Stripe customer (and have top-ups charged to them)."""

    name: str | None = Field(None, description="Name of the billing account")
    auto_top_up_enabled: bool | None = Field(
        None, description="Enable/disable automatic data top-ups"
    )
    monthly_top_up_cap_cents: int | None = Field(
        None,
        ge=0,
        description="Max automatic top-up spend per billing period, in cents",
    )


class InternalPatchBillingAccountRequest(PatchBillingAccountRequest):
    """Server-side superset used by the Stripe integration only."""

    stripe_customer_id: str | None = Field(
        None, description="Stripe customer ID associated with the billing account"
    )
    stripe_data: dict | None = Field(
        None, description="Stripe-related data for the billing account"
    )


class QueryBillingAccountRequest(BaseModel):
    name: str | None = Field(None, description="Name of the billing account")
    principal_account_id: str | None = Field(
        None, description="ID of the associated principal account"
    )


class ListBillingAccountResponse(BaseModel):
    items: list[BillingAccount] = Field(..., description="List of billing accounts")
    total: int = Field(..., description="Total number of matching billing accounts")
