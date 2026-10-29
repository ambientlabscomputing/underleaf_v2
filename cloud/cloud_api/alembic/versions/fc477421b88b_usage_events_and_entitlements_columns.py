"""usage_events and entitlements_buckets columns (track 5 phase A)

Adds the columns needed for idempotent, period-attributable usage
ingestion (stream_id, period_start/end, idempotency_key, billing_period_id)
and widens usage_amount / network_traffic_balance to BigInteger since byte
counts overflow a 4-byte int4 well within normal usage.

Safe to run against an empty usage_events table (nothing in this codebase
has ever written a usage event -- see track 5 investigation notes); if that
assumption ever stops holding, the NOT NULL columns added here will need a
backfill migration first.

Revision ID: fc477421b88b
Revises: 09441847ab0e
Create Date: 2026-09-08 00:00:00.000000

"""

from collections.abc import Sequence

import sqlalchemy as sa

from alembic import op

# revision identifiers, used by Alembic.
revision: str = "fc477421b88b"
down_revision: str | Sequence[str] | None = "09441847ab0e"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    """Upgrade schema."""
    op.alter_column(
        "usage_events",
        "usage_amount",
        type_=sa.BigInteger(),
        existing_type=sa.Integer(),
        schema="underleaf",
    )
    op.add_column(
        "usage_events",
        sa.Column("stream_id", sa.String(), nullable=True),
        schema="underleaf",
    )
    op.create_foreign_key(
        "fk_usage_events_stream_id_streams",
        "usage_events",
        "streams",
        ["stream_id"],
        ["id"],
        source_schema="underleaf",
        referent_schema="underleaf",
    )
    op.add_column(
        "usage_events",
        sa.Column("period_start", sa.DateTime(timezone=True), nullable=False),
        schema="underleaf",
    )
    op.add_column(
        "usage_events",
        sa.Column("period_end", sa.DateTime(timezone=True), nullable=False),
        schema="underleaf",
    )
    op.add_column(
        "usage_events",
        sa.Column("idempotency_key", sa.String(), nullable=False),
        schema="underleaf",
    )
    op.create_unique_constraint(
        "uq_usage_events_idempotency_key",
        "usage_events",
        ["idempotency_key"],
        schema="underleaf",
    )
    op.add_column(
        "usage_events",
        sa.Column("billing_period_id", sa.String(), nullable=True),
        schema="underleaf",
    )
    op.alter_column(
        "entitlements_buckets",
        "network_traffic_balance",
        type_=sa.BigInteger(),
        existing_type=sa.Integer(),
        schema="underleaf",
    )


def downgrade() -> None:
    """Downgrade schema."""
    op.alter_column(
        "entitlements_buckets",
        "network_traffic_balance",
        type_=sa.Integer(),
        existing_type=sa.BigInteger(),
        schema="underleaf",
    )
    op.drop_column("usage_events", "billing_period_id", schema="underleaf")
    op.drop_constraint(
        "uq_usage_events_idempotency_key",
        "usage_events",
        schema="underleaf",
        type_="unique",
    )
    op.drop_column("usage_events", "idempotency_key", schema="underleaf")
    op.drop_column("usage_events", "period_end", schema="underleaf")
    op.drop_column("usage_events", "period_start", schema="underleaf")
    op.drop_constraint(
        "fk_usage_events_stream_id_streams",
        "usage_events",
        schema="underleaf",
        type_="foreignkey",
    )
    op.drop_column("usage_events", "stream_id", schema="underleaf")
    op.alter_column(
        "usage_events",
        "usage_amount",
        type_=sa.Integer(),
        existing_type=sa.BigInteger(),
        schema="underleaf",
    )
