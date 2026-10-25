"""The pricing catalog: tiers, prices, allowances and overage policy.

Single source of truth. The `plans` / `plan_prices` tables are seeded from
this (alembic data migration) and linked to Stripe by
`python -m cloud_api.scripts.sync_stripe_catalog`; the free tier -- which
never goes through Stripe -- reads its limits straight from here.

Data sizes are decimal (1 GB = 10^9 bytes) to match how the tiers are sold.
See RFDs/RFD-4.md for the policy these encode.
"""

from dataclasses import dataclass
from datetime import UTC, datetime

GB = 10**9
TB = 10**12

# Prepaid data block a customer can buy on top of their plan, e.g. +100 GB
# for $5 on the $24 Builder plan.
TOP_UP_BLOCK_BYTES = 100 * GB
TOP_UP_PRICE_CENTS = 500

# "Small, temporary grace allowance" applied when a paid plan hits 100%:
# 2% of the plan's included data (floor 1 GB). Granted once per exhaustion
# event; cleared when a top-up or renewal replenishes the allowance.
GRACE_FRACTION = 0.02
GRACE_FLOOR_BYTES = 1 * GB

# Alert thresholds for paid plans (percent of allowance used).
USAGE_ALERT_THRESHOLDS = (80, 95, 100)

# Connection slots are not a priced dimension -- tiers are sold by data
# only -- so paid plans get a generous, effectively non-binding cap that
# still protects the platform from runaway connection creation.
FREE_CONNECTION_SLOTS = 1
PAID_CONNECTION_SLOTS = 25


@dataclass(frozen=True)
class PlanSpec:
    name: str
    price_cents: int
    included_bytes: int
    connection_slots: int
    paid: bool

    @property
    def grace_bytes(self) -> int:
        if not self.paid:
            return 0  # free tier hard-stops
        return max(int(self.included_bytes * GRACE_FRACTION), GRACE_FLOOR_BYTES)

    @property
    def top_up_block_bytes(self) -> int:
        return TOP_UP_BLOCK_BYTES if self.paid else 0

    @property
    def top_up_price_cents(self) -> int:
        return TOP_UP_PRICE_CENTS if self.paid else 0


FREE = PlanSpec("free", 0, 5 * GB, FREE_CONNECTION_SLOTS, paid=False)
MAKER = PlanSpec("maker", 900, 100 * GB, PAID_CONNECTION_SLOTS, paid=True)
BUILDER = PlanSpec("builder", 2400, 500 * GB, PAID_CONNECTION_SLOTS, paid=True)
STUDIO = PlanSpec("studio", 5900, 2 * TB, PAID_CONNECTION_SLOTS, paid=True)
FLEET = PlanSpec("fleet", 11900, 5 * TB, PAID_CONNECTION_SLOTS, paid=True)

PLANS: tuple[PlanSpec, ...] = (FREE, MAKER, BUILDER, STUDIO, FLEET)
PLANS_BY_NAME = {p.name: p for p in PLANS}


def month_bounds(now: datetime | None = None) -> tuple[datetime, datetime]:
    """Calendar-month [start, end) in UTC -- the free tier's period."""
    now = now or datetime.now(UTC)
    start = now.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
    if start.month == 12:
        end = start.replace(year=start.year + 1, month=1)
    else:
        end = start.replace(month=start.month + 1)
    return start, end
