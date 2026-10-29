from dataclasses import dataclass, field
from datetime import UTC, datetime

from cloud_api import logger
from cloud_api.lib import catalog
from cloud_api.models.api import (
    CreateEntitlementsBucketRequest,
    EntitlementsBucket,
    ListEntitlementsBucketResponse,
    PatchEntitlementsBucketRequest,
    QueryEntitlementsBucketRequest,
    UsageAlertKind,
)
from cloud_api.repository.entitlements_bucket_repository import (
    EntitlementsBalanceField,
    EntitlementsBucketRepository,
)
from cloud_api.repository.usage_alert_repository import UsageAlertRepository

# Alerts are keyed by allowance period; legacy buckets with no recorded
# period share this sentinel so de-duplication still works for them.
_NO_PERIOD = datetime(1970, 1, 1, tzinfo=UTC)

_THRESHOLD_KINDS = {
    80: UsageAlertKind.USAGE_80,
    95: UsageAlertKind.USAGE_95,
    100: UsageAlertKind.USAGE_100,
}


class EntitlementsBucketNotFoundError(Exception):
    def __init__(self, entitlements_bucket_id: str) -> None:
        super().__init__(f"EntitlementsBucket '{entitlements_bucket_id}' not found")
        self.entitlements_bucket_id = entitlements_bucket_id


class EntitlementsExceededError(Exception):
    """Raised when a reservation would take a hard cap below zero."""

    def __init__(
        self, billing_account_id: str, field: EntitlementsBalanceField
    ) -> None:
        super().__init__(
            f"BillingAccount '{billing_account_id}' has exhausted its {field.value}"
        )
        self.billing_account_id = billing_account_id
        self.field = field


@dataclass
class ChargeOutcome:
    """What a traffic charge did to the account, for the caller to act on."""

    bucket: EntitlementsBucket
    new_alerts: list[UsageAlertKind] = field(default_factory=list)
    grace_granted: bool = False

    @property
    def hard_stop(self) -> bool:
        """Relayed traffic must stop: nothing left, and no grace to fall back
        on (free tier, or the grace allowance is already spent)."""
        b = self.bucket
        if b.network_traffic_balance > 0 or b.grace_balance > 0:
            return False
        return b.grace_policy_bytes == 0 or b.grace_granted_at is not None


def percent_used(bucket: EntitlementsBucket) -> float:
    if bucket.network_traffic_allowance <= 0:
        return 0.0
    used = bucket.network_traffic_allowance - bucket.network_traffic_balance
    return max(0.0, used * 100.0 / bucket.network_traffic_allowance)


class EntitlementsBucketService:
    def __init__(
        self,
        entitlements_bucket_repo: EntitlementsBucketRepository,
        usage_alert_repo: UsageAlertRepository,
    ) -> None:
        self.entitlements_bucket_repo = entitlements_bucket_repo
        self.usage_alert_repo = usage_alert_repo

    # -- CRUD (internal; no customer-facing router) ---------------------------

    async def create_entitlements_bucket(
        self, req: CreateEntitlementsBucketRequest
    ) -> EntitlementsBucket:
        bucket = await self.entitlements_bucket_repo.create_entitlements_bucket(req)
        logger.bind(
            entitlements_bucket_id=bucket.id,
            billing_account_id=bucket.billing_account_id,
        ).info("EntitlementsBucket created")
        return bucket

    async def get_entitlements_buckets(
        self, req: QueryEntitlementsBucketRequest
    ) -> ListEntitlementsBucketResponse:
        items = await self.entitlements_bucket_repo.query_entitlements_buckets(req)
        return ListEntitlementsBucketResponse(items=items, total=len(items))

    async def get_entitlements_bucket(
        self, entitlements_bucket_id: str
    ) -> EntitlementsBucket:
        bucket = await self.entitlements_bucket_repo.get_entitlements_bucket(
            entitlements_bucket_id
        )
        if bucket is None:
            raise EntitlementsBucketNotFoundError(entitlements_bucket_id)
        return bucket

    async def get_entitlements_bucket_by_billing_account(
        self, billing_account_id: str
    ) -> EntitlementsBucket | None:
        """Returns None (not an error) when no bucket exists -- callers doing
        enforcement treat a missing bucket as fail-open and log loudly."""
        bucket = await self.entitlements_bucket_repo.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        if bucket is None:
            return None
        return await self._roll_free_period_if_due(bucket)

    async def patch_entitlements_bucket(
        self, entitlements_bucket_id: str, req: PatchEntitlementsBucketRequest
    ) -> EntitlementsBucket:
        bucket = await self.entitlements_bucket_repo.patch_entitlements_bucket(
            entitlements_bucket_id, req
        )
        if bucket is None:
            raise EntitlementsBucketNotFoundError(entitlements_bucket_id)
        return bucket

    async def delete_entitlements_bucket(self, entitlements_bucket_id: str) -> None:
        deleted = await self.entitlements_bucket_repo.delete_entitlements_bucket(
            entitlements_bucket_id
        )
        if not deleted:
            raise EntitlementsBucketNotFoundError(entitlements_bucket_id)

    # -- Periods & plans ------------------------------------------------------

    async def _roll_free_period_if_due(
        self, bucket: EntitlementsBucket
    ) -> EntitlementsBucket:
        """The free tier has no Stripe subscription to announce a renewal, so
        its monthly allowance is refilled lazily the first time the bucket is
        touched after the period ends. Paid buckets are only ever renewed by
        a paid invoice (see StripeWebhookService), never by the clock."""
        if bucket.stripe_managed_period:
            return bucket
        now = datetime.now(UTC)
        if bucket.period_end is not None and now < bucket.period_end:
            return bucket
        start, end = catalog.month_bounds(now)
        rolled = await self.entitlements_bucket_repo.apply_plan(
            bucket.id,
            allowance=catalog.FREE.included_bytes,
            slot_limit=catalog.FREE.connection_slots,
            grace_policy_bytes=catalog.FREE.grace_bytes,
            stripe_managed=False,
            reset_usage=True,
            period_start=start,
            period_end=end,
        )
        logger.bind(billing_account_id=bucket.billing_account_id).info(
            "Free allowance refilled for new month"
        )
        return rolled or bucket

    async def apply_plan(
        self,
        billing_account_id: str,
        *,
        allowance: int,
        slot_limit: int,
        grace_policy_bytes: int,
        stripe_managed: bool,
        reset_usage: bool,
        period_start: datetime | None = None,
        period_end: datetime | None = None,
    ) -> EntitlementsBucket | None:
        bucket = await self.entitlements_bucket_repo.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        if bucket is None:
            logger.bind(billing_account_id=billing_account_id).warning(
                "No entitlements bucket to apply plan to"
            )
            return None
        return await self.entitlements_bucket_repo.apply_plan(
            bucket.id,
            allowance=allowance,
            slot_limit=slot_limit,
            grace_policy_bytes=grace_policy_bytes,
            stripe_managed=stripe_managed,
            reset_usage=reset_usage,
            period_start=period_start,
            period_end=period_end,
        )

    async def credit_top_up(
        self, billing_account_id: str, top_up_bytes: int
    ) -> EntitlementsBucket | None:
        bucket = await self.entitlements_bucket_repo.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        if bucket is None:
            return None
        return await self.entitlements_bucket_repo.credit_top_up(
            bucket.id, top_up_bytes
        )

    # -- Connection slots -----------------------------------------------------

    async def check_and_reserve_connection_slot(self, billing_account_id: str) -> None:
        """Fail-open if no bucket exists (operational gap, not the caller's
        fault); fail-closed (raise) if a bucket exists but is full."""
        bucket = await self.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        if bucket is None:
            logger.bind(billing_account_id=billing_account_id).warning(
                "No entitlements bucket for billing account; allowing connection "
                "(fail-open)"
            )
            return
        updated = await self.entitlements_bucket_repo.decrement_balance(
            bucket.id, EntitlementsBalanceField.CONNECTION_SLOT, 1
        )
        if updated is None:
            raise EntitlementsExceededError(
                billing_account_id, EntitlementsBalanceField.CONNECTION_SLOT
            )

    async def release_connection_slot(self, billing_account_id: str) -> None:
        bucket = await self.entitlements_bucket_repo.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        if bucket is None:
            return
        await self.entitlements_bucket_repo.increment_balance(
            bucket.id, EntitlementsBalanceField.CONNECTION_SLOT, 1
        )

    async def reconcile_connection_slots(
        self, billing_account_id: str, open_connections: int
    ) -> None:
        bucket = await self.entitlements_bucket_repo.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        if bucket is not None:
            await self.entitlements_bucket_repo.set_slot_balance(
                bucket.id, open_connections
            )

    # -- Network traffic ------------------------------------------------------

    async def has_network_traffic_balance(self, billing_account_id: str) -> bool:
        """Gate for *new* streams. Grace counts: a paid customer running on
        their grace allowance may still open streams until it's spent.
        Fail-open if no bucket exists."""
        bucket = await self.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        if bucket is None:
            return True
        return bucket.network_traffic_balance > 0 or bucket.grace_balance > 0

    async def charge_network_traffic(
        self, billing_account_id: str, amount: int
    ) -> ChargeOutcome | None:
        """Charge already-transferred bytes against the allowance, then grace.

        Also detects the events the overage policy hangs off: usage-threshold
        crossings (80/95/100%, paid plans), the one-time grace grant at 100%,
        and whether relayed traffic must now be stopped. Returns None when
        there is nothing to charge against (no bucket / zero bytes).
        """
        if amount <= 0:
            return None
        bucket = await self.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        if bucket is None:
            logger.bind(billing_account_id=billing_account_id).warning(
                "No entitlements bucket for billing account; usage not charged"
            )
            return None

        updated = await self.entitlements_bucket_repo.draw_traffic(bucket.id, amount)
        if updated is None:
            return None

        grace_granted = False
        if updated.network_traffic_balance == 0:
            granted = await self.entitlements_bucket_repo.grant_grace(updated.id)
            if granted is not None:
                updated, grace_granted = granted, True
                # A single tick can carry the account *past* zero. That
                # overshoot was drawn before grace existed, so it was dropped;
                # charge it to the grace allowance now that there is one.
                overflow = amount - bucket.network_traffic_balance
                if overflow > 0:
                    redrawn = await self.entitlements_bucket_repo.draw_traffic(
                        updated.id, overflow
                    )
                    updated = redrawn or updated

        period = updated.period_start or _NO_PERIOD
        pct = percent_used(updated)
        new_alerts: list[UsageAlertKind] = []
        for threshold in catalog.USAGE_ALERT_THRESHOLDS:
            if pct < threshold:
                continue
            # 80/95 are paid-plan nudges; free just gets the 100% notice.
            if threshold < 100 and not updated.stripe_managed_period:
                continue
            kind = _THRESHOLD_KINDS[threshold]
            if await self.usage_alert_repo.record_if_new(
                billing_account_id, kind, period
            ):
                new_alerts.append(kind)

        return ChargeOutcome(
            bucket=updated, new_alerts=new_alerts, grace_granted=grace_granted
        )
