"""worker live index: DELETED + IN_PROGRESS is still live

`DELETING` is gone as a state: a teardown under way is `DELETED + IN_PROGRESS`
(the VM may still exist), and only `DELETED + SUCCEEDED` frees the per-type
slot. Rewrites any DELETING rows and rebuilds the partial unique index to match.

Revision ID: c83e0d4a6b15
Revises: a41f6c20d7e3
Create Date: 2026-10-31 00:00:00.000000

"""

from collections.abc import Sequence

import sqlalchemy as sa

from alembic import op

revision: str = "c83e0d4a6b15"
down_revision: str | Sequence[str] | None = "a41f6c20d7e3"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None

SCHEMA = "underleaf"
INDEX = "uq_workers_one_live_per_type"


def _rebuild_index(where: str) -> None:
    op.drop_index(INDEX, table_name="workers", schema=SCHEMA)
    op.create_index(
        INDEX,
        "workers",
        ["worker_type"],
        unique=True,
        schema=SCHEMA,
        postgresql_where=sa.text(where),
    )


def upgrade() -> None:
    """Upgrade schema."""
    op.execute(
        f"UPDATE {SCHEMA}.workers SET state = 'DELETED', status = 'in_progress' "
        "WHERE state = 'DELETING'"
    )
    # A DELETED worker that failed used to be DELETED + FAILED; the failure is
    # recorded in `error`, and DELETED + SUCCEEDED now means "gone".
    op.execute(
        f"UPDATE {SCHEMA}.workers SET status = 'succeeded' "
        "WHERE state = 'DELETED' AND status = 'failed'"
    )
    _rebuild_index("NOT (state = 'DELETED' AND status = 'succeeded')")


def downgrade() -> None:
    """Downgrade schema."""
    op.execute(
        f"UPDATE {SCHEMA}.workers SET state = 'DELETING' "
        "WHERE state = 'DELETED' AND status = 'in_progress'"
    )
    _rebuild_index("state <> 'DELETED'")
