"""pricing tiers, overage policy, usage alerts and top-ups

Schema for the overage policy (RFDs/RFD-4.md): per-period allowances with a
grace allowance, auto top-up settings, usage alerts, and top-up purchases.
Also seeds the plan catalog (Free / Maker / Builder / Studio / Fleet) and
backfills entitlements for accounts created before this existed.

Catalog numbers are inlined here (a snapshot), deliberately not imported from
cloud_api.lib.catalog, so this migration keeps meaning the same thing if the
live catalog later changes.

Revision ID: 2e503a78b645
Revises: cc6cf79a9c9c
Create Date: 2026-10-24 00:00:00.000000

"""

from collections.abc import Sequence
from datetime import UTC, datetime

import sqlalchemy as sa
from epyxid import XID

from alembic import op

revision: str = "2e503a78b645"
down_revision: str | Sequence[str] | None = "cc6cf79a9c9c"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None

GB = 10**9
TB = 10**12
TOP_UP_BLOCK_BYTES = 100 * GB
TOP_UP_PRICE_CENTS = 500

# name, price_cents, included_bytes, connection_slots, paid
SEED_PLANS = [
    ("free", 0, 5 * GB, 1, False),
    ("maker", 900, 100 * GB, 25, True),
    ("builder", 2400, 500 * GB, 25, True),
    ("studio", 5900, 2 * TB, 25, True),
    ("fleet", 11900, 5 * TB, 25, True),
]


def _grace(included: int, paid: bool) -> int:
    return max(int(included * 0.02), 1 * GB) if paid else 0


def _month_bounds(now: datetime) -> tuple[datetime, datetime]:
    start = now.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
    end = (
        start.replace(year=start.year + 1, month=1)
        if start.month == 12
        else start.replace(month=start.month + 1)
    )
    return start, end


def upgrade() -> None:
    # -- schema ---------------------------------------------------------------
    op.add_column(
        "billing_accounts",
        sa.Column(
            "auto_top_up_enabled", sa.Boolean(), nullable=False, server_default="false"
        ),
        schema="underleaf",
    )
    op.add_column(
        "billing_accounts",
        sa.Column(
            "monthly_top_up_cap_cents",
            sa.Integer(),
            nullable=False,
            server_default="5000",
        ),
        schema="underleaf",
    )

    for name, type_, extra in [
        ("network_traffic_allowance", sa.BigInteger(), {"server_default": "0"}),
        ("connection_slot_limit", sa.Integer(), {"server_default": "0"}),
        ("grace_policy_bytes", sa.BigInteger(), {"server_default": "0"}),
        ("grace_balance", sa.BigInteger(), {"server_default": "0"}),
    ]:
        op.add_column(
            "entitlements_buckets",
            sa.Column(name, type_, nullable=False, **extra),
            schema="underleaf",
        )
    for name in ("grace_granted_at", "period_start", "period_end"):
        op.add_column(
            "entitlements_buckets",
            sa.Column(name, sa.DateTime(timezone=True), nullable=True),
            schema="underleaf",
        )
    op.add_column(
        "entitlements_buckets",
        sa.Column(
            "stripe_managed_period",
            sa.Boolean(),
            nullable=False,
            server_default="false",
        ),
        schema="underleaf",
    )

    op.add_column(
        "plan_prices",
        sa.Column("grace_bytes", sa.BigInteger(), nullable=False, server_default="0"),
        schema="underleaf",
    )
    op.add_column(
        "plan_prices",
        sa.Column(
            "top_up_block_bytes", sa.BigInteger(), nullable=False, server_default="0"
        ),
        schema="underleaf",
    )
    op.add_column(
        "plan_prices",
        sa.Column(
            "top_up_price_cents", sa.Integer(), nullable=False, server_default="0"
        ),
        schema="underleaf",
    )

    op.create_table(
        "usage_alerts",
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("billing_account_id", sa.String(), nullable=False),
        sa.Column("kind", sa.String(), nullable=False),
        sa.Column("period_start", sa.DateTime(timezone=True), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["billing_account_id"], ["underleaf.billing_accounts.id"]
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "billing_account_id", "kind", "period_start", name="uq_usage_alerts_once"
        ),
        schema="underleaf",
    )
    op.create_table(
        "top_ups",
        sa.Column("id", sa.String(), nullable=False),
        sa.Column("billing_account_id", sa.String(), nullable=False),
        sa.Column("bytes", sa.BigInteger(), nullable=False),
        sa.Column("amount_cents", sa.Integer(), nullable=False),
        sa.Column("source", sa.String(), nullable=False),
        sa.Column("status", sa.String(), nullable=False),
        sa.Column("stripe_invoice_id", sa.String(), nullable=True),
        sa.Column("period_start", sa.DateTime(timezone=True), nullable=False),
        sa.Column("failure_reason", sa.String(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["billing_account_id"], ["underleaf.billing_accounts.id"]
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("stripe_invoice_id"),
        schema="underleaf",
    )

    # -- data -----------------------------------------------------------------
    bind = op.get_bind()
    now = datetime.now(UTC)
    month_start, month_end = _month_bounds(now)

    # 'pro' was a placeholder tier that never had a price; the closest of the
    # new tiers keeps any such row readable.
    bind.execute(
        sa.text("UPDATE underleaf.subscriptions SET tier = 'studio' WHERE tier = 'pro'")
    )

    # Seed the catalog (idempotent on plan name).
    for name, price_cents, included, slots, paid in SEED_PLANS:
        existing = bind.execute(
            sa.text("SELECT id FROM underleaf.plans WHERE name = :n"), {"n": name}
        ).scalar()
        if existing is None:
            existing = f"plan_{XID()}"
            bind.execute(
                sa.text(
                    "INSERT INTO underleaf.plans (id, name, is_active, created_at, updated_at) "
                    "VALUES (:id, :n, true, :now, :now)"
                ),
                {"id": existing, "n": name, "now": now},
            )
        has_price = bind.execute(
            sa.text(
                "SELECT 1 FROM underleaf.plan_prices WHERE plan_id = :p AND interval = 'month'"
            ),
            {"p": existing},
        ).scalar()
        if not has_price:
            bind.execute(
                sa.text(
                    "INSERT INTO underleaf.plan_prices (id, plan_id, interval, unit_amount_cents, "
                    "currency, is_active, network_traffic_limit_bytes, connection_slot_limit, "
                    "grace_bytes, top_up_block_bytes, top_up_price_cents, created_at, updated_at) "
                    "VALUES (:id, :p, 'month', :cents, 'usd', true, :bytes, :slots, :grace, "
                    ":tub, :tup, :now, :now)"
                ),
                {
                    "id": f"price_{XID()}",
                    "p": existing,
                    "cents": price_cents,
                    "bytes": included,
                    "slots": slots,
                    "grace": _grace(included, paid),
                    "tub": TOP_UP_BLOCK_BYTES if paid else 0,
                    "tup": TOP_UP_PRICE_CENTS if paid else 0,
                    "now": now,
                },
            )

    # Buckets that predate the allowance columns. Free accounts get this
    # month's free allowance; accounts already on Stripe keep their balance
    # and become Stripe-managed.
    bind.execute(
        sa.text(
            """
            UPDATE underleaf.entitlements_buckets b SET
                network_traffic_allowance = :free_bytes,
                network_traffic_balance = :free_bytes,
                connection_slot_limit = 1,
                connection_slot_balance = 1,
                grace_policy_bytes = 0,
                period_start = :ps,
                period_end = :pe
            WHERE NOT EXISTS (
                SELECT 1 FROM underleaf.subscriptions s
                WHERE s.billing_account_id = b.billing_account_id
                  AND s.stripe_subscription_id IS NOT NULL
            )
            """
        ),
        {"free_bytes": 5 * GB, "ps": month_start, "pe": month_end},
    )
    bind.execute(
        sa.text(
            """
            UPDATE underleaf.entitlements_buckets b SET
                network_traffic_allowance = GREATEST(b.network_traffic_balance, 0),
                connection_slot_limit = b.connection_slot_balance,
                stripe_managed_period = true,
                period_start = :now
            WHERE EXISTS (
                SELECT 1 FROM underleaf.subscriptions s
                WHERE s.billing_account_id = b.billing_account_id
                  AND s.stripe_subscription_id IS NOT NULL
            )
            """
        ),
        {"now": now},
    )

    # Billing accounts that never got a bucket would have been unmetered
    # (enforcement fails open on a missing bucket); give them the free tier.
    missing = (
        bind.execute(
            sa.text(
                "SELECT ba.id FROM underleaf.billing_accounts ba "
                "WHERE NOT EXISTS (SELECT 1 FROM underleaf.entitlements_buckets b "
                "WHERE b.billing_account_id = ba.id)"
            )
        )
        .scalars()
        .all()
    )
    for billing_account_id in missing:
        bind.execute(
            sa.text(
                "INSERT INTO underleaf.entitlements_buckets (id, name, billing_account_id, "
                "network_traffic_balance, connection_slot_balance, network_traffic_allowance, "
                "connection_slot_limit, grace_policy_bytes, grace_balance, period_start, "
                "period_end, stripe_managed_period, created_at, updated_at) "
                "VALUES (:id, 'Default Entitlements Bucket', :ba, :b, 1, :b, 1, 0, 0, :ps, :pe, "
                "false, :now, :now)"
            ),
            {
                "id": f"entitlements_{XID()}",
                "ba": billing_account_id,
                "b": 5 * GB,
                "ps": month_start,
                "pe": month_end,
                "now": now,
            },
        )


def downgrade() -> None:
    bind = op.get_bind()
    seeded = [p[0] for p in SEED_PLANS]
    bind.execute(
        sa.text(
            "UPDATE underleaf.subscriptions SET plan_price_id = NULL WHERE plan_price_id IN "
            "(SELECT pp.id FROM underleaf.plan_prices pp JOIN underleaf.plans p ON p.id = "
            "pp.plan_id WHERE p.name = ANY(:names))"
        ),
        {"names": seeded},
    )
    bind.execute(
        sa.text(
            "DELETE FROM underleaf.plan_prices WHERE plan_id IN "
            "(SELECT id FROM underleaf.plans WHERE name = ANY(:names))"
        ),
        {"names": seeded},
    )
    bind.execute(
        sa.text("DELETE FROM underleaf.plans WHERE name = ANY(:names)"),
        {"names": seeded},
    )

    op.drop_table("top_ups", schema="underleaf")
    op.drop_table("usage_alerts", schema="underleaf")
    for col in ("top_up_price_cents", "top_up_block_bytes", "grace_bytes"):
        op.drop_column("plan_prices", col, schema="underleaf")
    for col in (
        "stripe_managed_period",
        "period_end",
        "period_start",
        "grace_granted_at",
        "grace_balance",
        "grace_policy_bytes",
        "connection_slot_limit",
        "network_traffic_allowance",
    ):
        op.drop_column("entitlements_buckets", col, schema="underleaf")
    op.drop_column("billing_accounts", "monthly_top_up_cap_cents", schema="underleaf")
    op.drop_column("billing_accounts", "auto_top_up_enabled", schema="underleaf")
