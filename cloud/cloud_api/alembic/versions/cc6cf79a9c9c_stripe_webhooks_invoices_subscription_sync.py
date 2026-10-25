"""subscriptions Stripe sync columns, invoices, stripe_webhook_events (track 5 phase D)

Revision ID: cc6cf79a9c9c
Revises: 92e00c2bc32d
Create Date: 2026-09-08 00:00:03.000000

"""

from collections.abc import Sequence

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

# revision identifiers, used by Alembic.
revision: str = "cc6cf79a9c9c"
down_revision: str | Sequence[str] | None = "92e00c2bc32d"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    """Upgrade schema."""
    op.add_column(
        "subscriptions",
        sa.Column("plan_price_id", sa.String(), nullable=True),
        schema="underleaf",
    )
    op.create_foreign_key(
        "fk_subscriptions_plan_price_id_plan_prices",
        "subscriptions",
        "plan_prices",
        ["plan_price_id"],
        ["id"],
        source_schema="underleaf",
        referent_schema="underleaf",
    )
    op.add_column(
        "subscriptions",
        sa.Column("stripe_subscription_id", sa.String(), nullable=True),
        schema="underleaf",
    )
    op.create_unique_constraint(
        "uq_subscriptions_stripe_subscription_id",
        "subscriptions",
        ["stripe_subscription_id"],
        schema="underleaf",
    )
    op.add_column(
        "subscriptions",
        sa.Column("stripe_status", sa.String(), nullable=True),
        schema="underleaf",
    )
    op.add_column(
        "subscriptions",
        sa.Column("current_period_start", sa.DateTime(timezone=True), nullable=True),
        schema="underleaf",
    )
    op.add_column(
        "subscriptions",
        sa.Column("current_period_end", sa.DateTime(timezone=True), nullable=True),
        schema="underleaf",
    )
    op.add_column(
        "subscriptions",
        sa.Column(
            "cancel_at_period_end",
            sa.Boolean(),
            nullable=False,
            server_default="false",
        ),
        schema="underleaf",
    )

    op.create_table(
        "invoices",
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("billing_account_id", sa.String(), nullable=False),
        sa.Column("stripe_invoice_id", sa.String(), nullable=False),
        sa.Column("status", sa.String(), nullable=False),
        sa.Column("amount_due_cents", sa.Integer(), nullable=False),
        sa.Column("amount_paid_cents", sa.Integer(), nullable=False),
        sa.Column("currency", sa.String(), nullable=False),
        sa.Column("hosted_invoice_url", sa.String(), nullable=True),
        sa.Column("invoice_pdf_url", sa.String(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["billing_account_id"],
            ["underleaf.billing_accounts.id"],
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("stripe_invoice_id"),
        schema="underleaf",
    )

    op.create_table(
        "stripe_webhook_events",
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("stripe_event_id", sa.String(), nullable=False),
        sa.Column("event_type", sa.String(), nullable=False),
        sa.Column(
            "payload",
            postgresql.JSONB(astext_type=sa.Text()),
            nullable=False,
        ),
        sa.Column("processed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("processing_error", sa.String(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("stripe_event_id"),
        schema="underleaf",
    )


def downgrade() -> None:
    """Downgrade schema."""
    op.drop_table("stripe_webhook_events", schema="underleaf")
    op.drop_table("invoices", schema="underleaf")
    op.drop_column("subscriptions", "cancel_at_period_end", schema="underleaf")
    op.drop_column("subscriptions", "current_period_end", schema="underleaf")
    op.drop_column("subscriptions", "current_period_start", schema="underleaf")
    op.drop_column("subscriptions", "stripe_status", schema="underleaf")
    op.drop_constraint(
        "uq_subscriptions_stripe_subscription_id",
        "subscriptions",
        schema="underleaf",
        type_="unique",
    )
    op.drop_column("subscriptions", "stripe_subscription_id", schema="underleaf")
    op.drop_constraint(
        "fk_subscriptions_plan_price_id_plan_prices",
        "subscriptions",
        schema="underleaf",
        type_="foreignkey",
    )
    op.drop_column("subscriptions", "plan_price_id", schema="underleaf")
