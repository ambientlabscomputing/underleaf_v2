from datetime import datetime
from typing import Any, ClassVar

from sqlalchemy import BigInteger, DateTime, ForeignKey, UniqueConstraint
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, relationship

from cloud_api.models.base import IDPrefix, generate_id


class SQLBase(DeclarativeBase):
    id: Mapped[str] = mapped_column(primary_key=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    updated_at: Mapped[datetime] = mapped_column(DateTime(timezone=True))


class SQLPrincipalAccount(SQLBase):
    __tablename__ = "principal_accounts"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.PRINCIPAL_ACCOUNT)
    )
    name: Mapped[str]
    users: Mapped[list["SQLUser"]] = relationship(back_populates="principal_account")
    billing_account: Mapped["SQLBillingAccount"] = relationship(
        back_populates="principal_account"
    )


class SQLBillingAccount(SQLBase):
    __tablename__ = "billing_accounts"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.BILLING_ACCOUNT)
    )
    name: Mapped[str]
    principal_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.principal_accounts.id")
    )
    stripe_customer_id: Mapped[str | None] = mapped_column(nullable=True)
    stripe_data: Mapped[dict] = mapped_column(JSONB, default={}, server_default="{}")
    auto_top_up_enabled: Mapped[bool] = mapped_column(
        default=False, server_default="false"
    )
    monthly_top_up_cap_cents: Mapped[int] = mapped_column(
        default=5000, server_default="5000"
    )
    principal_account: Mapped["SQLPrincipalAccount"] = relationship(
        back_populates="billing_account"
    )
    entitlements_bucket: Mapped["SQLEntitlementsBucket"] = relationship(
        back_populates="billing_account"
    )
    subscription: Mapped["SQLSubscription"] = relationship(
        back_populates="billing_account"
    )


class SQLSubscription(SQLBase):
    __tablename__ = "subscriptions"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.SUBSCRIPTION)
    )
    billing_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.billing_accounts.id")
    )
    # Display/filter convenience, derived from plan_price.plan.name. Once
    # plan_price_id is set, this is written only by the Stripe webhook
    # handler -- see SubscriptionService.patch_subscription's guard against
    # a client PATCH changing tier out from under an active Stripe sub.
    tier: Mapped[str] = mapped_column()
    plan_price_id: Mapped[str | None] = mapped_column(
        ForeignKey("underleaf.plan_prices.id"), nullable=True
    )
    stripe_subscription_id: Mapped[str | None] = mapped_column(
        unique=True, nullable=True
    )
    # Stripe's own status enum (active/past_due/canceled/trialing/...), kept
    # verbatim rather than reinterpreted into a new enum.
    stripe_status: Mapped[str | None] = mapped_column(nullable=True)
    current_period_start: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    current_period_end: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    cancel_at_period_end: Mapped[bool] = mapped_column(default=False)
    billing_account: Mapped["SQLBillingAccount"] = relationship(
        back_populates="subscription"
    )


class SQLEntitlementsBucket(SQLBase):
    __tablename__ = "entitlements_buckets"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.ENTITLEMENTS_BUCKET)
    )
    name: Mapped[str]
    billing_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.billing_accounts.id")
    )
    billing_account: Mapped["SQLBillingAccount"] = relationship(
        back_populates="entitlements_bucket"
    )

    # BigInteger: byte counts overflow a 4-byte int4 well within normal usage.
    network_traffic_balance: Mapped[int] = mapped_column(BigInteger, default=0)
    connection_slot_balance: Mapped[int] = mapped_column(default=0)
    # Bytes available this period (included + credited top-ups): the
    # denominator for "% used" and the baseline plan changes preserve usage
    # against.
    network_traffic_allowance: Mapped[int] = mapped_column(
        BigInteger, default=0, server_default="0"
    )
    connection_slot_limit: Mapped[int] = mapped_column(default=0, server_default="0")
    # Temporary grace allowance, granted once per exhaustion event (0 = the
    # plan hard-stops, e.g. free tier). grace_granted_at is cleared when a
    # top-up or renewal replenishes the allowance.
    grace_policy_bytes: Mapped[int] = mapped_column(
        BigInteger, default=0, server_default="0"
    )
    grace_balance: Mapped[int] = mapped_column(
        BigInteger, default=0, server_default="0"
    )
    grace_granted_at: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    period_start: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    period_end: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    # Paid plans renew via Stripe (invoice.paid); free renews lazily on the
    # calendar month because there is no Stripe subscription to drive it.
    stripe_managed_period: Mapped[bool] = mapped_column(
        default=False, server_default="false"
    )


class SQLUser(SQLBase):
    __tablename__ = "users"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.USER)
    )
    name: Mapped[str]
    email: Mapped[str]
    principal_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.principal_accounts.id")
    )
    principal_account: Mapped["SQLPrincipalAccount"] = relationship(
        back_populates="users"
    )
    password: Mapped["SQLUserPassword"] = relationship(
        uselist=False, back_populates="user", cascade="all, delete-orphan"
    )


class SQLUserPassword(SQLBase):
    __tablename__ = "user_passwords"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.CREDENTIAL)
    )
    user_id: Mapped[str] = mapped_column(ForeignKey("underleaf.users.id"))
    password_hash: Mapped[str] = mapped_column()
    user: Mapped["SQLUser"] = relationship(back_populates="password")


class SQLCluster(SQLBase):
    __tablename__ = "clusters"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    name: Mapped[str]
    principal_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.principal_accounts.id")
    )
    manager_node_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.nodes.id"), nullable=True
    )
    nodes: Mapped[list["SQLNode"]] = relationship(
        back_populates="cluster",
        foreign_keys="SQLNode.cluster_id",
    )


class SQLNode(SQLBase):
    __tablename__ = "nodes"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    name: Mapped[str]
    cluster_id: Mapped[str] = mapped_column(ForeignKey("underleaf.clusters.id"))
    cluster: Mapped["SQLCluster"] = relationship(
        back_populates="nodes",
        foreign_keys="[SQLNode.cluster_id]",
    )


class SQLConnection(SQLBase):
    __tablename__ = "connections"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.CONNECTION)
    )
    node_id: Mapped[str] = mapped_column(ForeignKey("underleaf.nodes.id"))
    name: Mapped[str]
    state: Mapped[str]
    status: Mapped[str]
    closed_at: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    streams: Mapped[list["SQLStream"]] = relationship(back_populates="connection")


class SQLStream(SQLBase):
    __tablename__ = "streams"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.STREAM)
    )
    name: Mapped[str]
    connection_id: Mapped[str] = mapped_column(ForeignKey("underleaf.connections.id"))
    type: Mapped[str]
    state: Mapped[str]
    status: Mapped[str]
    endpoint: Mapped[str | None] = mapped_column(nullable=True)
    port: Mapped[int | None] = mapped_column(nullable=True)
    closed_at: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    connection: Mapped["SQLConnection"] = relationship(back_populates="streams")
    # Cumulative bytes (bytes_in + bytes_out, as reported by conn_worker)
    # already turned into a usage_events row for this stream -- the
    # baseline the next sync computes its delta against. Internal
    # bookkeeping only, not client-settable via PatchStreamRequest.
    last_reported_bytes: Mapped[int] = mapped_column(BigInteger, default=0)


class SQLPlan(SQLBase):
    __tablename__ = "plans"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.PLAN)
    )
    name: Mapped[str] = mapped_column(unique=True)
    stripe_product_id: Mapped[str | None] = mapped_column(nullable=True)
    is_active: Mapped[bool] = mapped_column(default=True)
    prices: Mapped[list["SQLPlanPrice"]] = relationship(back_populates="plan")


class SQLPlanPrice(SQLBase):
    __tablename__ = "plan_prices"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.PLAN_PRICE)
    )
    plan_id: Mapped[str] = mapped_column(ForeignKey("underleaf.plans.id"))
    plan: Mapped["SQLPlan"] = relationship(back_populates="prices")
    # Unique + indexed: this is the join key a Stripe webhook payload's
    # price ID resolves back to one of our plans through.
    stripe_price_id: Mapped[str | None] = mapped_column(unique=True, nullable=True)
    interval: Mapped[str] = mapped_column()
    unit_amount_cents: Mapped[int] = mapped_column()
    currency: Mapped[str] = mapped_column(default="usd")
    is_active: Mapped[bool] = mapped_column(default=True)
    network_traffic_limit_bytes: Mapped[int] = mapped_column(BigInteger)
    connection_slot_limit: Mapped[int] = mapped_column()
    grace_bytes: Mapped[int] = mapped_column(BigInteger, default=0, server_default="0")
    top_up_block_bytes: Mapped[int] = mapped_column(
        BigInteger, default=0, server_default="0"
    )
    top_up_price_cents: Mapped[int] = mapped_column(default=0, server_default="0")


class SQLInvoice(SQLBase):
    __tablename__ = "invoices"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.INVOICE)
    )
    billing_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.billing_accounts.id")
    )
    stripe_invoice_id: Mapped[str] = mapped_column(unique=True)
    # Mirrors Stripe's own invoice status enum verbatim (draft/open/paid/
    # void/uncollectible) rather than reinterpreting it into a new one.
    status: Mapped[str] = mapped_column()
    amount_due_cents: Mapped[int] = mapped_column()
    amount_paid_cents: Mapped[int] = mapped_column()
    currency: Mapped[str] = mapped_column(default="usd")
    hosted_invoice_url: Mapped[str | None] = mapped_column(nullable=True)
    invoice_pdf_url: Mapped[str | None] = mapped_column(nullable=True)


class SQLStripeWebhookEvent(SQLBase):
    __tablename__ = "stripe_webhook_events"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.WEBHOOK_EVENT)
    )
    # Stripe's own event ID (evt_...) -- the idempotency key. A retried
    # delivery for an event already recorded here is a no-op.
    stripe_event_id: Mapped[str] = mapped_column(unique=True)
    event_type: Mapped[str] = mapped_column()
    payload: Mapped[dict] = mapped_column(JSONB)
    processed_at: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    processing_error: Mapped[str | None] = mapped_column(nullable=True)


class SQLUsageAlert(SQLBase):
    __tablename__ = "usage_alerts"
    __table_args__: ClassVar[tuple[Any, ...]] = (
        # One alert per kind per allowance period -- the unique constraint is
        # what makes "notify exactly once when crossing 80%" race-safe.
        UniqueConstraint(
            "billing_account_id", "kind", "period_start", name="uq_usage_alerts_once"
        ),
        {"schema": "underleaf"},
    )

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.ALERT)
    )
    billing_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.billing_accounts.id")
    )
    kind: Mapped[str] = mapped_column()
    period_start: Mapped[datetime] = mapped_column(DateTime(timezone=True))


class SQLTopUp(SQLBase):
    __tablename__ = "top_ups"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.TOP_UP)
    )
    billing_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.billing_accounts.id")
    )
    bytes: Mapped[int] = mapped_column(BigInteger)
    amount_cents: Mapped[int] = mapped_column()
    source: Mapped[str] = mapped_column()
    status: Mapped[str] = mapped_column()
    stripe_invoice_id: Mapped[str | None] = mapped_column(unique=True, nullable=True)
    period_start: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    failure_reason: Mapped[str | None] = mapped_column(nullable=True)


class SQLUsageEvent(SQLBase):
    __tablename__ = "usage_events"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.USAGE_EVENT)
    )
    billing_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.billing_accounts.id")
    )
    event_type: Mapped[str] = mapped_column()
    usage_unit: Mapped[str] = mapped_column()
    # BigInteger: byte counts overflow a 4-byte int4 well within normal usage.
    usage_amount: Mapped[int] = mapped_column(BigInteger)
    # Source stream, when the event was derived from stream activity.
    stream_id: Mapped[str | None] = mapped_column(
        ForeignKey("underleaf.streams.id"), nullable=True
    )
    # Window this event's usage_amount covers -- needed to attribute a
    # still-open stream's usage to the right billing period unambiguously
    # (created_at alone is ambiguous once a stream can span period boundaries).
    period_start: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    period_end: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    # Caller-supplied dedupe key (e.g. "{stream_id}:{prev_total}:{new_total}")
    # unique-constrained so a retried ingestion can never double-count.
    idempotency_key: Mapped[str] = mapped_column(unique=True)
    # Set once a rollup job assigns this event to a closed billing period;
    # NULL means "current period, not yet rolled up."
    billing_period_id: Mapped[str | None] = mapped_column(nullable=True)


class SQLClusterCandidate(SQLBase):
    __tablename__ = "cluster_candidates"
    __table_args__: ClassVar[dict[str, str]] = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.CANDIDATE)
    )
    # user-facing short code carried in the verification URL
    user_code: Mapped[str] = mapped_column(unique=True)
    # SHA-256 hex digest of the plaintext device_code (never stored in clear)
    device_code_hash: Mapped[str] = mapped_column(unique=True)
    status: Mapped[str] = mapped_column(
        default="pending"
    )  # pending|approved|expired|consumed
    proposed_cluster_name: Mapped[str]
    proposed_cluster_id: Mapped[str]
    # filled in when a user approves the request
    principal_account_id: Mapped[str | None] = mapped_column(nullable=True)
    cluster_id: Mapped[str | None] = mapped_column(
        ForeignKey("underleaf.clusters.id"), nullable=True
    )
    node_id: Mapped[str | None] = (
        mapped_column(  # for node-specific registration requestsd
            ForeignKey("underleaf.nodes.id"), nullable=True
        )
    )
    # one-time token used by orch-server to request a signed cert
    one_time_token_hash: Mapped[str | None] = mapped_column(nullable=True)
    one_time_token_expires_at: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    device_code_expires_at: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    poll_interval: Mapped[int] = mapped_column(default=5)
    last_polled_at: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
