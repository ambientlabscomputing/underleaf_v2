"""worker_settings

Operator overrides of a few worker settings (kill switch, idle grace, ...),
so a pause survives restarts and reaches every process.

Revision ID: a41f6c20d7e3
Revises: 7d3a1c5e9b04
Create Date: 2026-10-31 00:00:00.000000

"""

from collections.abc import Sequence

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision: str = "a41f6c20d7e3"
down_revision: str | Sequence[str] | None = "7d3a1c5e9b04"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "worker_settings",
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("value", postgresql.JSONB(astext_type=sa.Text()), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
        schema="underleaf",
    )


def downgrade() -> None:
    op.drop_table("worker_settings", schema="underleaf")
