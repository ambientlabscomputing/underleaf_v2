from pydantic import BaseModel, Field

from cloud_api.models.base import Base, IDPrefix, generate_id


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
