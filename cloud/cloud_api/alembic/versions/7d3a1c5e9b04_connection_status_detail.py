"""connections.status_detail

Progress/failure text for connections that are PENDING (queued for a worker).

Revision ID: 7d3a1c5e9b04
Revises: 5b21991f8d72
Create Date: 2026-10-30 00:00:00.000000

"""

from collections.abc import Sequence

import sqlalchemy as sa

from alembic import op

revision: str = "7d3a1c5e9b04"
down_revision: str | Sequence[str] | None = "5b21991f8d72"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column(
        "connections",
        sa.Column("status_detail", sa.String(), nullable=True),
        schema="underleaf",
    )


def downgrade() -> None:
    op.drop_column("connections", "status_detail", schema="underleaf")
