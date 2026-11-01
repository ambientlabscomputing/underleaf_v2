"""workers, worker_tasks, worker_events

Schema for on-demand conn_worker VM provisioning:
- workers: one row per VM, with a `state` + `status` pair for the state machine
  and a partial unique index allowing at most one live (non-DELETED) worker per
  type, which is what stops concurrent requests booting duplicate VMs.
- worker_tasks: the durable queue (claimed with FOR UPDATE SKIP LOCKED + lease).
- worker_events: append-only timeline for visibility/debugging.

Revision ID: 5b21991f8d72
Revises: 2e503a78b645
Create Date: 2026-10-30 00:00:00.000000

"""

from collections.abc import Sequence

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision: str = "5b21991f8d72"
down_revision: str | Sequence[str] | None = "2e503a78b645"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None

SCHEMA = "underleaf"


def _base_columns() -> list[sa.Column]:
    return [
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
    ]


def upgrade() -> None:
    """Upgrade schema."""
    op.create_table(
        "workers",
        sa.Column("worker_type", sa.String(), nullable=False),
        sa.Column("state", sa.String(), nullable=False),
        sa.Column("status", sa.String(), nullable=False),
        sa.Column("error", sa.String(), nullable=True),
        sa.Column("state_changed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column(
            "extra_data",
            postgresql.JSONB(astext_type=sa.Text()),
            server_default="{}",
            nullable=False,
        ),
        sa.Column(
            "config",
            postgresql.JSONB(astext_type=sa.Text()),
            server_default="{}",
            nullable=False,
        ),
        *_base_columns(),
        sa.PrimaryKeyConstraint("id"),
        schema=SCHEMA,
    )
    op.create_index(
        "uq_workers_one_live_per_type",
        "workers",
        ["worker_type"],
        unique=True,
        schema=SCHEMA,
        postgresql_where=sa.text("state <> 'DELETED'"),
    )

    op.create_table(
        "worker_tasks",
        sa.Column("kind", sa.String(), nullable=False),
        sa.Column("state", sa.String(), nullable=False),
        sa.Column(
            "payload",
            postgresql.JSONB(astext_type=sa.Text()),
            server_default="{}",
            nullable=False,
        ),
        sa.Column("connection_id", sa.String(), nullable=True),
        sa.Column("worker_id", sa.String(), nullable=True),
        sa.Column("attempts", sa.Integer(), server_default="0", nullable=False),
        sa.Column("max_attempts", sa.Integer(), server_default="5", nullable=False),
        sa.Column("run_after", sa.DateTime(timezone=True), nullable=False),
        sa.Column("lease_expires_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("error", sa.String(), nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        *_base_columns(),
        sa.PrimaryKeyConstraint("id"),
        schema=SCHEMA,
    )
    op.create_index(
        "ix_worker_tasks_state_run_after",
        "worker_tasks",
        ["state", "run_after"],
        schema=SCHEMA,
    )

    op.create_table(
        "worker_events",
        sa.Column("worker_id", sa.String(), nullable=True),
        sa.Column("task_id", sa.String(), nullable=True),
        sa.Column("connection_id", sa.String(), nullable=True),
        sa.Column("type", sa.String(), nullable=False),
        sa.Column("message", sa.String(), server_default="", nullable=False),
        sa.Column(
            "data",
            postgresql.JSONB(astext_type=sa.Text()),
            server_default="{}",
            nullable=False,
        ),
        *_base_columns(),
        sa.PrimaryKeyConstraint("id"),
        schema=SCHEMA,
    )
    op.create_index(
        "ix_worker_events_worker_id_created_at",
        "worker_events",
        ["worker_id", "created_at"],
        schema=SCHEMA,
    )
    op.create_index(
        "ix_worker_events_connection_id_created_at",
        "worker_events",
        ["connection_id", "created_at"],
        schema=SCHEMA,
    )


def downgrade() -> None:
    """Downgrade schema."""
    op.drop_table("worker_events", schema=SCHEMA)
    op.drop_table("worker_tasks", schema=SCHEMA)
    op.drop_table("workers", schema=SCHEMA)
