from pydantic import BaseModel, Field

from cloud_api.models.base import Base, IDPrefix, generate_id


class Plan(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.PLAN),
        description="Unique identifier for the plan",
    )
    name: str = Field(..., description="Name of the plan (e.g. 'free', 'builder')")
    stripe_product_id: str | None = Field(
        default=None, description="Associated Stripe product ID"
    )
    is_active: bool = Field(default=True, description="Whether the plan is offered")


class CreatePlanRequest(BaseModel):
    name: str = Field(..., description="Name of the plan")
    stripe_product_id: str | None = Field(
        default=None, description="Associated Stripe product ID"
    )
    is_active: bool = Field(default=True, description="Whether the plan is offered")


class PatchPlanRequest(BaseModel):
    name: str | None = Field(None, description="Name of the plan")
    stripe_product_id: str | None = Field(
        None, description="Associated Stripe product ID"
    )
    is_active: bool | None = Field(None, description="Whether the plan is offered")


class QueryPlanRequest(BaseModel):
    name: str | None = Field(None, description="Name of the plan")
    is_active: bool | None = Field(None, description="Whether the plan is offered")


class ListPlanResponse(BaseModel):
    items: list[Plan] = Field(..., description="List of plans")
    total: int = Field(..., description="Total number of matching plans")


class PlanPrice(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.PLAN_PRICE),
        description="Unique identifier for the plan price",
    )
    plan_id: str = Field(..., description="ID of the associated plan")
    stripe_price_id: str | None = Field(
        default=None, description="Associated Stripe price ID"
    )
    interval: str = Field(..., description="Billing interval ('month' or 'year')")
    unit_amount_cents: int = Field(..., description="Price in cents")
    currency: str = Field(default="usd", description="ISO currency code")
    is_active: bool = Field(default=True, description="Whether the price is offered")
    network_traffic_limit_bytes: int = Field(
        ..., description="Network traffic entitlement this price grants, in bytes"
    )
    connection_slot_limit: int = Field(
        ..., description="Connection slot entitlement this price grants"
    )
    grace_bytes: int = Field(
        default=0, description="Temporary grace allowance granted at 100% usage"
    )
    top_up_block_bytes: int = Field(
        default=0, description="Size of one prepaid top-up block (0 = no top-ups)"
    )
    top_up_price_cents: int = Field(
        default=0, description="Price of one top-up block, in cents"
    )


class CreatePlanPriceRequest(BaseModel):
    plan_id: str = Field(..., description="ID of the associated plan")
    stripe_price_id: str | None = Field(
        default=None, description="Associated Stripe price ID"
    )
    interval: str = Field(..., description="Billing interval ('month' or 'year')")
    unit_amount_cents: int = Field(..., description="Price in cents")
    currency: str = Field(default="usd", description="ISO currency code")
    is_active: bool = Field(default=True, description="Whether the price is offered")
    network_traffic_limit_bytes: int = Field(
        ..., description="Network traffic entitlement this price grants, in bytes"
    )
    connection_slot_limit: int = Field(
        ..., description="Connection slot entitlement this price grants"
    )
    grace_bytes: int = Field(default=0)
    top_up_block_bytes: int = Field(default=0)
    top_up_price_cents: int = Field(default=0)


class PatchPlanPriceRequest(BaseModel):
    stripe_price_id: str | None = Field(None, description="Associated Stripe price ID")
    is_active: bool | None = Field(None, description="Whether the price is offered")
    unit_amount_cents: int | None = Field(None, description="Price in cents")
    network_traffic_limit_bytes: int | None = Field(
        None, description="Network traffic entitlement this price grants, in bytes"
    )
    connection_slot_limit: int | None = Field(
        None, description="Connection slot entitlement this price grants"
    )


class QueryPlanPriceRequest(BaseModel):
    plan_id: str | None = Field(None, description="ID of the associated plan")
    stripe_price_id: str | None = Field(None, description="Associated Stripe price ID")
    is_active: bool | None = Field(None, description="Whether the price is offered")


class ListPlanPriceResponse(BaseModel):
    items: list[PlanPrice] = Field(..., description="List of plan prices")
    total: int = Field(..., description="Total number of matching plan prices")
