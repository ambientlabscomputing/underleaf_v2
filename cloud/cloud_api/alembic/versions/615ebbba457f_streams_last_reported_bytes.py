"""streams.last_reported_bytes (track 5 phase B)

Internal bookkeeping column: cumulative bytes already turned into a
usage_events row for a stream, so sync_usage_from_conn_worker can compute a
delta against conn_worker's reported running total instead of re-emitting
the full amount every sync.

Revision ID: 615ebbba457f
Revises: fc477421b88b
Create Date: 2026-09-08 00:00:01.000000

"""

from collections.abc import Sequence

import sqlalchemy as sa

from alembic import op

# revision identifiers, used by Alembic.
revision: str = "615ebbba457f"
down_revision: str | Sequence[str] | None = "fc477421b88b"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    """Upgrade schema."""
    op.add_column(
        "streams",
        sa.Column(
            "last_reported_bytes",
            sa.BigInteger(),
            nullable=False,
            server_default="0",
        ),
        schema="underleaf",
    )


def downgrade() -> None:
    """Downgrade schema."""
    op.drop_column("streams", "last_reported_bytes", schema="underleaf")
