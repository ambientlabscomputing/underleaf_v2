"""ufctl device login requests and device keys (RFD 5 step 7b)

device_login_requests holds the pending RFC 8628 grants started by
`ufctl login`; device_keys holds the revocable bearer secrets they produce
(stored only as a SHA-256 hash).

Revision ID: 7b3d1f9a4c52
Revises: 2e503a78b645
Create Date: 2026-11-08 00:00:00.000000

"""

from collections.abc import Sequence

import sqlalchemy as sa

from alembic import op

# revision identifiers, used by Alembic.
revision: str = "7b3d1f9a4c52"
down_revision: str | Sequence[str] | None = "2e503a78b645"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    """Upgrade schema."""
    op.create_table(
        "device_login_requests",
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("user_code", sa.String(), nullable=False),
        sa.Column("device_code_hash", sa.String(), nullable=False),
        sa.Column("status", sa.String(), nullable=False),
        sa.Column("device_name", sa.String(), nullable=False),
        sa.Column("principal_account_id", sa.String(), nullable=True),
        sa.Column("expires_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("poll_interval", sa.Integer(), nullable=False),
        sa.Column("last_polled_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["principal_account_id"], ["underleaf.principal_accounts.id"]
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("user_code"),
        sa.UniqueConstraint("device_code_hash"),
        schema="underleaf",
    )
    op.create_table(
        "device_keys",
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("principal_account_id", sa.String(), nullable=False),
        sa.Column("name", sa.String(), nullable=False),
        sa.Column("key_hash", sa.String(), nullable=False),
        sa.Column("last_used_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("revoked_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["principal_account_id"], ["underleaf.principal_accounts.id"]
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("key_hash"),
        schema="underleaf",
    )
    op.create_index(
        op.f("ix_underleaf_device_keys_principal_account_id"),
        "device_keys",
        ["principal_account_id"],
        unique=False,
        schema="underleaf",
    )


def downgrade() -> None:
    """Downgrade schema."""
    op.drop_index(
        op.f("ix_underleaf_device_keys_principal_account_id"),
        table_name="device_keys",
        schema="underleaf",
    )
    op.drop_table("device_keys", schema="underleaf")
    op.drop_table("device_login_requests", schema="underleaf")
