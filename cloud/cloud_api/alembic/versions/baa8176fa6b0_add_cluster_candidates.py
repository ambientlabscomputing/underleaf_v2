"""add_cluster_candidates

Revision ID: baa8176fa6b0
Revises: 89ca30dc3bb0
Create Date: 2026-06-20 00:59:59.843074

"""

from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

# revision identifiers, used by Alembic.
revision: str = "baa8176fa6b0"
down_revision: Union[str, Sequence[str], None] = "89ca30dc3bb0"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    """Upgrade schema."""
    op.create_table(
        "cluster_candidates",
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("user_code", sa.String(), nullable=False),
        sa.Column("device_code_hash", sa.String(), nullable=False),
        sa.Column("status", sa.String(), nullable=False),
        sa.Column("proposed_cluster_name", sa.String(), nullable=False),
        sa.Column("proposed_cluster_id", sa.String(), nullable=False),
        sa.Column("principal_account_id", sa.String(), nullable=True),
        sa.Column("cluster_id", sa.String(), nullable=True),
        sa.Column("one_time_token_hash", sa.String(), nullable=True),
        sa.Column(
            "one_time_token_expires_at", sa.DateTime(timezone=True), nullable=True
        ),
        sa.Column("device_code_expires_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("poll_interval", sa.Integer(), nullable=False),
        sa.Column("last_polled_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["cluster_id"], ["underleaf.clusters.id"]),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("device_code_hash"),
        sa.UniqueConstraint("user_code"),
        schema="underleaf",
    )


def downgrade() -> None:
    """Downgrade schema."""
    op.drop_table("cluster_candidates", schema="underleaf")
