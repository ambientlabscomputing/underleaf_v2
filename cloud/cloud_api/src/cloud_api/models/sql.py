from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, relationship
from sqlalchemy import ForeignKey, DateTime
from sqlalchemy.dialects.postgresql import JSONB
from datetime import datetime
from typing import Optional
from cloud_api.models.base import generate_id, IDPrefix


class SQLBase(DeclarativeBase):
    id: Mapped[str] = mapped_column(primary_key=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    updated_at: Mapped[datetime] = mapped_column(DateTime(timezone=True))


class SQLPrincipalAccount(SQLBase):
    __tablename__ = "principal_accounts"
    __table_args__ = {"schema": "underleaf"}

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
    __table_args__ = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.BILLING_ACCOUNT)
    )
    name: Mapped[str]
    principal_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.principal_accounts.id")
    )
    stripe_customer_id: Mapped[Optional[str]] = mapped_column(nullable=True)
    stripe_data: Mapped[dict] = mapped_column(JSONB, default={}, server_default="{}")
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
    __table_args__ = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.SUBSCRIPTION)
    )
    billing_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.billing_accounts.id")
    )
    tier: Mapped[str] = mapped_column()
    billing_account: Mapped["SQLBillingAccount"] = relationship(
        back_populates="subscription"
    )


class SQLEntitlementsBucket(SQLBase):
    __tablename__ = "entitlements_buckets"
    __table_args__ = {"schema": "underleaf"}

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

    network_traffic_balance: Mapped[int] = mapped_column(default=0)
    connection_slot_balance: Mapped[int] = mapped_column(default=0)


class SQLUser(SQLBase):
    __tablename__ = "users"
    __table_args__ = {"schema": "underleaf"}

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
    __table_args__ = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.CREDENTIAL)
    )
    user_id: Mapped[str] = mapped_column(ForeignKey("underleaf.users.id"))
    password_hash: Mapped[str] = mapped_column()
    user: Mapped["SQLUser"] = relationship(back_populates="password")


class SQLCluster(SQLBase):
    __tablename__ = "clusters"
    __table_args__ = {"schema": "underleaf"}

    name: Mapped[str]
    principal_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.principal_accounts.id")
    )
    manager_node_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.nodes.id"), nullable=True
    )
    nodes: Mapped[list["SQLNode"]] = relationship(back_populates="cluster")


class SQLNode(SQLBase):
    __tablename__ = "nodes"
    __table_args__ = {"schema": "underleaf"}

    name: Mapped[str]
    cluster_id: Mapped[str] = mapped_column(ForeignKey("underleaf.clusters.id"))
    cluster: Mapped["SQLCluster"] = relationship(back_populates="nodes")


class SQLConnection(SQLBase):
    __tablename__ = "connections"
    __table_args__ = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.CONNECTION)
    )
    node_id: Mapped[str] = mapped_column(ForeignKey("underleaf.nodes.id"))
    name: Mapped[str]
    state: Mapped[str]
    status: Mapped[str]
    closed_at: Mapped[Optional[datetime]] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    streams: Mapped[list["SQLStream"]] = relationship(back_populates="connection")


class SQLStream(SQLBase):
    __tablename__ = "streams"
    __table_args__ = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.STREAM)
    )
    name: Mapped[str]
    connection_id: Mapped[str] = mapped_column(ForeignKey("underleaf.connections.id"))
    type: Mapped[str]
    state: Mapped[str]
    status: Mapped[str]
    endpoint: Mapped[Optional[str]] = mapped_column(nullable=True)
    port: Mapped[Optional[int]] = mapped_column(nullable=True)
    closed_at: Mapped[Optional[datetime]] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    connection: Mapped["SQLConnection"] = relationship(back_populates="streams")


class SQLUsageEvent(SQLBase):
    __tablename__ = "usage_events"
    __table_args__ = {"schema": "underleaf"}

    id: Mapped[str] = mapped_column(
        primary_key=True, default=lambda: generate_id(IDPrefix.USAGE_EVENT)
    )
    billing_account_id: Mapped[str] = mapped_column(
        ForeignKey("underleaf.billing_accounts.id")
    )
    event_type: Mapped[str] = mapped_column()
    usage_unit: Mapped[str] = mapped_column()
    usage_amount: Mapped[int] = mapped_column()


class SQLClusterCandidate(SQLBase):
    __tablename__ = "cluster_candidates"
    __table_args__ = {"schema": "underleaf"}

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
    principal_account_id: Mapped[Optional[str]] = mapped_column(nullable=True)
    cluster_id: Mapped[Optional[str]] = mapped_column(
        ForeignKey("underleaf.clusters.id"), nullable=True
    )
    node_id: Mapped[Optional[str]] = mapped_column( # for node-specific registration requestsd
        ForeignKey("underleaf.nodes.id"), nullable=True
    )
    # one-time token used by orch-server to request a signed cert
    one_time_token_hash: Mapped[Optional[str]] = mapped_column(nullable=True)
    one_time_token_expires_at: Mapped[Optional[datetime]] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    device_code_expires_at: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    poll_interval: Mapped[int] = mapped_column(default=5)
    last_polled_at: Mapped[Optional[datetime]] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
