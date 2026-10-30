from datetime import datetime
from enum import StrEnum

from pydantic import Field

from cloud_api.models.base import Base, IDPrefix, generate_id


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
