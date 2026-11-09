"""API models for `ufctl` remote access (RFD 5): the RFC 8628 device login and
the device keys it produces. The wire contract for the two unauthenticated
endpoints is edge/cmd/ufctl/remoteaccess/doc.go."""

from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, Field

from cloud_api.models.base import Base, IDPrefix, generate_id

DEVICE_CODE_GRANT = "urn:ietf:params:oauth:grant-type:device_code"


class DeviceLoginStatus(StrEnum):
    PENDING = "pending"
    APPROVED = "approved"
    DENIED = "denied"
    CONSUMED = "consumed"


class UfctlDeviceRequest(BaseModel):
    """Sent by `ufctl login` to start the flow."""

    device_name: str = Field(
        default="",
        max_length=256,
        description="Human-readable name of the machine (its hostname)",
    )


class UfctlDeviceResponse(BaseModel):
    """RFC 8628 section 3.2 device authorization response."""

    device_code: str = Field(..., description="Secret code, kept by ufctl only")
    user_code: str = Field(..., description="Short code the owner confirms")
    verification_uri: str = Field(..., description="Account UI page to open")
    verification_uri_complete: str = Field(
        ..., description="verification_uri with the user_code appended"
    )
    expires_in: int = Field(..., description="Lifetime of the codes in seconds")
    interval: int = Field(..., description="Minimum polling interval in seconds")


class UfctlTokenRequest(BaseModel):
    grant_type: str = Field(default=DEVICE_CODE_GRANT)
    device_code: str = Field(..., max_length=256)


class UfctlTokenResponse(BaseModel):
    """Returned exactly once, to the poll that follows the approval."""

    device_key_id: str = Field(..., description="Id of the device key (dk_...)")
    device_key: str = Field(
        ..., description="The secret (ufdk_...). Shown once, never retrievable."
    )


class DeviceLoginRequestInfo(BaseModel):
    """What the account UI shows before the owner approves or denies."""

    user_code: str
    device_name: str
    status: DeviceLoginStatus
    created_at: datetime
    expires_at: datetime


class DeviceLoginDecisionRequest(BaseModel):
    user_code: str = Field(..., max_length=64)


class DeviceLoginDecisionResponse(BaseModel):
    status: DeviceLoginStatus
    device_name: str


class DeviceKey(Base):
    """A device key as shown to its owner. The secret and its hash are never
    part of this model."""

    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.DEVICE_KEY),
        description="Unique identifier (dk_...)",
    )
    principal_account_id: str
    name: str = Field(..., description="Device name given at login")
    last_used_at: datetime | None = None
    revoked_at: datetime | None = None


class ListDeviceKeysResponse(BaseModel):
    items: list[DeviceKey]
    total: int
