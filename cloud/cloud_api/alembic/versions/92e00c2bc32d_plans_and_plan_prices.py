"""plans and plan_prices (track 5 phase C)

The pricing catalog Stripe Checkout sells from and webhooks resolve
stripe_price_id back into -- replaces the hardcoded SubscriptionTier enum
as the source of truth for what tiers exist and what they grant.

Revision ID: 92e00c2bc32d
Revises: 615ebbba457f
Create Date: 2026-09-08 00:00:02.000000

"""

from collections.abc import Sequence

import sqlalchemy as sa

from alembic import op

# revision identifiers, used by Alembic.
revision: str = "92e00c2bc32d"
down_revision: str | Sequence[str] | None = "615ebbba457f"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    """Upgrade schema."""
    op.create_table(
        "plans",
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("name", sa.String(), nullable=False),
        sa.Column("stripe_product_id", sa.String(), nullable=True),
        sa.Column("is_active", sa.Boolean(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("name"),
        schema="underleaf",
    )
    op.create_table(
        "plan_prices",
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("plan_id", sa.String(), nullable=False),
        sa.Column("stripe_price_id", sa.String(), nullable=True),
        sa.Column("interval", sa.String(), nullable=False),
        sa.Column("unit_amount_cents", sa.Integer(), nullable=False),
        sa.Column("currency", sa.String(), nullable=False),
        sa.Column("is_active", sa.Boolean(), nullable=False),
        sa.Column("network_traffic_limit_bytes", sa.BigInteger(), nullable=False),
        sa.Column("connection_slot_limit", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["plan_id"],
            ["underleaf.plans.id"],
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("stripe_price_id"),
        schema="underleaf",
    )


def downgrade() -> None:
    """Downgrade schema."""
    op.drop_table("plan_prices", schema="underleaf")
    op.drop_table("plans", schema="underleaf")
