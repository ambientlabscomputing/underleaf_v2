from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, Field

from cloud_api.models.base import Base, IDPrefix, generate_id


class Status(StrEnum):
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    IN_PROGRESS = "in_progress"


class PrincipalAccount(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.PRINCIPAL_ACCOUNT),
        description="Unique identifier for the principal account",
    )
    name: str = Field(..., description="Name of the principal account")


class CreatePrincipalAccountRequest(BaseModel):
    name: str = Field(..., description="Name of the principal account")


class PatchPrincipalAccountRequest(BaseModel):
    name: str | None = Field(None, description="Name of the principal account")


class QueryPrincipalAccountRequest(BaseModel):
    name: str | None = Field(None, description="Name of the principal account")


class User(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.USER),
        description="Unique identifier for the user",
    )
    name: str = Field(..., description="Name of the user")
    email: str = Field(..., description="Email address of the user")
    principal_account_id: str = Field(
        ..., description="ID of the associated principal account"
    )


class CreateUserRequest(BaseModel):
    name: str = Field(..., description="Name of the user")
    password: str = Field(..., description="Password for the user")
    email: str = Field(..., description="Email address of the user")


class PatchUserRequest(BaseModel):
    name: str | None = Field(None, description="Name of the user")
    email: str | None = Field(None, description="Email address of the user")


class QueryUserRequest(BaseModel):
    name: str | None = Field(None, description="Name of the user")
    email: str | None = Field(None, description="Email address of the user")


class UserPassword(Base):
    user_id: str = Field(..., description="ID of the associated user")
    password_hash: str = Field(..., description="Hashed password for the user")


class InternalUser(User):
    user_password: UserPassword = Field(
        ..., description="Password information for the user"
    )


# Cluster and Node IDs don't get generators because they are tracked by the local clusters
# themselves and will come with a canonical ID.
class Cluster(Base):
    id: str = Field(
        ...,
        description="Unique identifier for the cluster",
    )
    name: str = Field(..., description="Name of the cluster")
    principal_account_id: str = Field(
        ..., description="ID of the associated principal account"
    )
    manager_node_id: str | None = Field(
        None, description="ID of the manager node for the cluster"
    )


class CreateClusterRequest(BaseModel):
    id: str = Field(..., description="Unique identifier for the cluster")
    name: str = Field(..., description="Name of the cluster")
    principal_account_id: str | None = Field(
        None,
        description="Principal account ID — injected server-side, not required in request body",
    )


class PatchClusterRequest(BaseModel):
    name: str | None = Field(None, description="Name of the cluster")


class QueryClusterRequest(BaseModel):
    name: str | None = Field(None, description="Name of the cluster")
    principal_account_id: str | None = Field(
        None, description="ID of the associated principal account"
    )


class ListClusterResponse(BaseModel):
    items: list["Cluster"] = Field(..., description="List of clusters")
    total: int = Field(..., description="Total number of matching clusters")


class Node(Base):
    id: str = Field(
        ...,
        description="Unique identifier for the node",
    )
    name: str = Field(..., description="Name of the node")
    cluster_id: str = Field(..., description="ID of the associated cluster")


class CreateNodeRequest(BaseModel):
    id: str = Field(..., description="Unique identifier for the node")
    name: str = Field(..., description="Name of the node")
    cluster_id: str = Field(..., description="ID of the associated cluster")


class PatchNodeRequest(BaseModel):
    name: str | None = Field(None, description="Name of the node")


class QueryNodeRequest(BaseModel):
    name: str | None = Field(None, description="Name of the node")
    cluster_id: str | None = Field(None, description="ID of the associated cluster")


class StreamType(StrEnum):
    SSH = "ssh"
    GW = "gateway"
    Tunnel = "tunnel"


class CreateConnectionRequest(BaseModel):
    name: str = Field(..., description="Name of the connection")
    node_id: str = Field(..., description="ID of the associated node")


class NewStreamRequest(BaseModel):
    connection_id: str = Field(..., description="ID of the associated connection")
    type_: StreamType = Field(..., description="Type of the stream", alias="type")
    endpoint: str | None = Field(None, description="Optional endpoint for the stream")
    port: int | None = Field(None, description="Optional port for the stream")


class TerminateConnRequest(BaseModel):
    connection_id: str = Field(..., description="ID of the connection to terminate")


class TerminateConnResponse(BaseModel):
    id: str = Field(..., description="ID of the terminated connection")
    status: str = Field(..., description="Status of the termination request")


class CloseStreamRequest(BaseModel):
    stream_id: str = Field(..., description="ID of the stream to close")


class CloseStreamResponse(BaseModel):
    id: str = Field(..., description="ID of the closed stream")
    status: str = Field(..., description="Status of the close request")


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


class CreateCheckoutSessionRequest(BaseModel):
    plan_price_id: str = Field(
        ..., description="ID of the plan price to subscribe the billing account to"
    )


class CreateCheckoutSessionResponse(BaseModel):
    url: str = Field(..., description="Stripe-hosted Checkout URL to redirect to")


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


class Invoice(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.INVOICE),
        description="Unique identifier for the invoice",
    )
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    stripe_invoice_id: str = Field(..., description="Associated Stripe invoice ID")
    status: str = Field(..., description="Stripe's own invoice status")
    amount_due_cents: int = Field(..., description="Amount due, in cents")
    amount_paid_cents: int = Field(..., description="Amount paid, in cents")
    currency: str = Field(default="usd", description="ISO currency code")
    hosted_invoice_url: str | None = Field(
        default=None, description="Stripe-hosted invoice page URL"
    )
    invoice_pdf_url: str | None = Field(
        default=None, description="Stripe invoice PDF URL"
    )


class UpsertInvoiceRequest(BaseModel):
    """Internal write surface for the Stripe webhook handler only -- there
    is no client-facing create/patch for invoices, they're a read-only
    mirror of Stripe's own invoice objects."""

    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    stripe_invoice_id: str = Field(..., description="Associated Stripe invoice ID")
    status: str = Field(..., description="Stripe's own invoice status")
    amount_due_cents: int = Field(..., description="Amount due, in cents")
    amount_paid_cents: int = Field(..., description="Amount paid, in cents")
    currency: str = Field(default="usd", description="ISO currency code")
    hosted_invoice_url: str | None = Field(
        default=None, description="Stripe-hosted invoice page URL"
    )
    invoice_pdf_url: str | None = Field(
        default=None, description="Stripe invoice PDF URL"
    )


class QueryInvoiceRequest(BaseModel):
    billing_account_id: str | None = Field(
        None, description="ID of the associated billing account"
    )


class ListInvoiceResponse(BaseModel):
    items: list[Invoice] = Field(..., description="List of invoices")
    total: int = Field(..., description="Total number of matching invoices")


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


class TopUpStatus(StrEnum):
    PENDING = "pending"
    PAID = "paid"
    FAILED = "failed"


class TopUpSource(StrEnum):
    AUTO = "auto"
    MANUAL = "manual"


class TopUp(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.TOP_UP),
        description="Unique identifier for the top-up",
    )
    billing_account_id: str = Field(..., description="Associated billing account")
    bytes: int = Field(..., description="Data block size purchased")
    amount_cents: int = Field(..., description="Price charged, in cents")
    source: TopUpSource = Field(..., description="auto (overage policy) or manual")
    status: TopUpStatus = Field(default=TopUpStatus.PENDING)
    stripe_invoice_id: str | None = Field(default=None)
    period_start: datetime = Field(..., description="Billing period it counts against")
    failure_reason: str | None = Field(default=None)


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


class UserSignUpResponse(BaseModel):
    user: User = Field(..., description="The created user")
    principal_account: PrincipalAccount = Field(
        ..., description="The created principal account"
    )
    billing_account: BillingAccount = Field(
        ..., description="The created billing account"
    )
    subscription: Subscription = Field(..., description="The created subscription")


class SignInRequest(BaseModel):
    email: str = Field(..., description="Email address of the user")
    password: str = Field(..., description="Password for the user")


class SignInResponse(BaseModel):
    user: User = Field(..., description="The authenticated user")


class TokenResponse(BaseModel):
    access_token: str = Field(..., description="Short-lived JWT access token")
    refresh_token: str = Field(..., description="Long-lived JWT refresh token")
    token_type: str = Field(default="bearer", description="Token type")


# ---------------------------------------------------------------------------
# Device Registration (RFC 8628 device authorization grant)
# ---------------------------------------------------------------------------


class CandidateStatus(StrEnum):
    PENDING = "pending"
    APPROVED = "approved"
    EXPIRED = "expired"
    CONSUMED = "consumed"


class RegisterDeviceRequest(BaseModel):
    """Sent by orch-server to initiate the device-auth flow."""

    proposed_cluster_name: str = Field(
        ..., description="Human-readable name for the cluster"
    )
    proposed_cluster_id: str = Field(
        ..., description="Stable ID pre-generated by orch-server"
    )


class DeviceAuthResponse(BaseModel):
    """RFC 8628 §3.2 device authorization response."""

    device_code: str = Field(
        ..., description="Secret opaque code, kept by orch-server only"
    )
    user_code: str = Field(..., description="Short code the user enters / URL carries")
    verification_uri: str = Field(..., description="Base URI for the account UI")
    verification_uri_complete: str = Field(
        ..., description="verification_uri with user_code appended"
    )
    expires_in: int = Field(
        ..., description="Lifetime of device_code/user_code in seconds"
    )
    interval: int = Field(default=5, description="Minimum polling interval in seconds")


class PollTokenRequest(BaseModel):
    """Sent by orch-server while polling for user approval."""

    grant_type: str = Field(default="urn:ietf:params:oauth:grant-type:device_code")
    device_code: str = Field(
        ..., description="The device_code returned by /registration/device"
    )


class PollTokenResponse(BaseModel):
    """Returned once the user has approved the registration."""

    one_time_cluster_token: str = Field(
        ..., description="Short-lived token for CSR signing"
    )
    cluster_id: str = Field(..., description="ID of the created Cluster")


class ClusterCandidate(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.CANDIDATE),
        description="Unique identifier for the candidate",
    )
    user_code: str = Field(..., description="Short human-readable code")
    status: CandidateStatus = Field(..., description="Current status of the candidate")
    proposed_cluster_name: str = Field(..., description="Proposed cluster name")
    proposed_cluster_id: str = Field(..., description="Proposed cluster ID")
    principal_account_id: str | None = Field(None, description="Set on approval")
    cluster_id: str | None = Field(None, description="Set on approval")


class ApproveCandidateRequest(BaseModel):
    user_code: str = Field(..., description="user_code from the registration URL")


class ApproveCandidateResponse(BaseModel):
    cluster: Cluster = Field(..., description="The newly created cluster")


class IssueCertificateRequest(BaseModel):
    csr_pem: str = Field(
        ..., description="PEM-encoded PKCS#10 certificate signing request"
    )
    node_id: str = Field(
        ..., description="Optional node ID for node-specific registration"
    )


class IssueCertificateResponse(BaseModel):
    certificate_pem: str = Field(
        ..., description="Signed client certificate in PEM format"
    )
    node_certificate_pem: str = Field(
        ..., description="Signed node certificate in PEM format"
    )
    ca_chain_pem: str = Field(..., description="Root CA certificate in PEM format")


class RenewCertificateRequest(BaseModel):
    """Like IssueCertificateRequest, but for an already-registered caller
    renewing its cert ahead of expiry. No one-time token: the caller's
    identity comes from the still-valid mTLS certificate it authenticated
    this request with (see get_access_claims / X-Subject-Id), not from a
    consumable candidate token.
    """

    csr_pem: str = Field(
        ..., description="PEM-encoded PKCS#10 certificate signing request"
    )
    node_id: str = Field(
        ..., description="Node ID to also issue a node certificate for"
    )
