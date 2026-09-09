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


class CreateBillingAccountRequest(BaseModel):
    name: str = Field(..., description="Name of the billing account")
    principal_account_id: str = Field(
        ..., description="ID of the associated principal account"
    )


class PatchBillingAccountRequest(BaseModel):
    name: str | None = Field(None, description="Name of the billing account")
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


class CreateEntitlementsBucketRequest(BaseModel):
    name: str = Field(..., description="Name of the entitlements bucket")
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )


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


class CreateUsageEventRequest(BaseModel):
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    event_type: UsageEventType = Field(..., description="Type of the usage event")
    usage_unit: UsageUnits = Field(..., description="Units of usage for the event")
    usage_amount: int = Field(..., description="Amount of usage for the event")


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


class SubscriptionTier(StrEnum):
    FREE = "free"
    BUILDER = "builder"
    PRO = "pro"


class Subscription(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.SUBSCRIPTION),
        description="Unique identifier for the subscription",
    )
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    tier: SubscriptionTier = Field(..., description="Subscription tier")


class CreateSubscriptionRequest(BaseModel):
    billing_account_id: str = Field(
        ..., description="ID of the associated billing account"
    )
    tier: SubscriptionTier = Field(..., description="Subscription tier")


class PatchSubscriptionRequest(BaseModel):
    tier: SubscriptionTier | None = Field(None, description="Subscription tier")


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
