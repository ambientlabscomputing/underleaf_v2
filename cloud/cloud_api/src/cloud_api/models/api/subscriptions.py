from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, Field

from cloud_api.models.base import Base, IDPrefix, generate_id


class SubscriptionTier(StrEnum):
    FREE = "free"
    MAKER = "maker"
    BUILDER = "builder"
    STUDIO = "studio"
    FLEET = "fleet"


class Subscription(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.SUBSCRIPTION),
        description="Unique identifier for the subscription",
    )
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    tier: SubscriptionTier = Field(..., description="Subscription tier")
    plan_price_id: str | None = Field(
        default=None, description="ID of the plan price this subscription is on"
    )
    stripe_subscription_id: str | None = Field(
        default=None, description="Associated Stripe subscription ID"
    )
    stripe_status: str | None = Field(
        default=None, description="Stripe's own subscription status"
    )
    current_period_start: datetime | None = Field(
        default=None, description="Start of the current Stripe billing period"
    )
    current_period_end: datetime | None = Field(
        default=None, description="End of the current Stripe billing period"
    )
    cancel_at_period_end: bool = Field(
        default=False, description="Whether the subscription cancels at period end"
    )


class CreateSubscriptionRequest(BaseModel):
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    tier: SubscriptionTier = Field(..., description="Subscription tier")


class PatchSubscriptionRequest(BaseModel):
    tier: SubscriptionTier | None = Field(None, description="Subscription tier")


class StripeSyncSubscriptionRequest(BaseModel):
    """Fields the Stripe webhook handler (and only the webhook handler) is
    allowed to write -- kept separate from PatchSubscriptionRequest, which is
    the client-facing PATCH surface, so the two can never be confused at a
    call site."""

    tier: SubscriptionTier | None = Field(None, description="Subscription tier")
    plan_price_id: str | None = Field(None, description="ID of the plan price")
    stripe_subscription_id: str | None = Field(
        None, description="Associated Stripe subscription ID"
    )
    stripe_status: str | None = Field(None, description="Stripe's own status")
    current_period_start: datetime | None = Field(
        None, description="Start of the current Stripe billing period"
    )
    current_period_end: datetime | None = Field(
        None, description="End of the current Stripe billing period"
    )
    cancel_at_period_end: bool | None = Field(
        None, description="Whether the subscription cancels at period end"
    )


class QuerySubscriptionRequest(BaseModel):
    billing_account_id: str | None = Field(
        None, description="ID of the associated billing account"
    )
    tier: SubscriptionTier | None = Field(None, description="Subscription tier")
    principal_account_id: str | None = Field(
        None, description="Scope to a principal account"
    )


class ListSubscriptionResponse(BaseModel):
    items: list[Subscription] = Field(..., description="List of subscriptions")
    total: int = Field(..., description="Total number of matching subscriptions")


class CreateCheckoutSessionRequest(BaseModel):
    plan_price_id: str = Field(
        ..., description="ID of the plan price to subscribe the billing account to"
    )


class CreateCheckoutSessionResponse(BaseModel):
    url: str = Field(..., description="Stripe-hosted Checkout URL to redirect to")
