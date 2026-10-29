from datetime import UTC, datetime

from cloud_api.models.api import (
    SubscriptionTier,
    UsageSummary,
)
from cloud_api.repository.billing_account_repository import BillingAccountRepository
from cloud_api.repository.plan_price_repository import PlanPriceRepository
from cloud_api.repository.subscription_repository import SubscriptionRepository
from cloud_api.repository.top_up_repository import TopUpRepository
from cloud_api.repository.usage_alert_repository import UsageAlertRepository
from cloud_api.service.entitlements_bucket_service import (
    EntitlementsBucketService,
    percent_used,
)
from cloud_api.service.top_up_service import GOOD_STANDING_STATUSES

_NO_PERIOD = datetime(1970, 1, 1, tzinfo=UTC)


class UsageSummaryNotAvailableError(Exception):
    """The billing account has no entitlements bucket to summarise."""


class UsageSummaryService:
    """Read model behind the customer's usage panel: where they are against
    their allowance, what the overage policy will do next, and the alerts
    raised this period."""

    def __init__(
        self,
        billing_account_repo: BillingAccountRepository,
        subscription_repo: SubscriptionRepository,
        plan_price_repo: PlanPriceRepository,
        top_up_repo: TopUpRepository,
        usage_alert_repo: UsageAlertRepository,
        entitlements_bucket_service: EntitlementsBucketService,
    ) -> None:
        self.billing_account_repo = billing_account_repo
        self.subscription_repo = subscription_repo
        self.plan_price_repo = plan_price_repo
        self.top_up_repo = top_up_repo
        self.usage_alert_repo = usage_alert_repo
        self.bucket_service = entitlements_bucket_service

    async def get_summary(self, billing_account_id: str) -> UsageSummary:
        account = await self.billing_account_repo.get_billing_account(
            billing_account_id
        )
        bucket = await self.bucket_service.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        if account is None or bucket is None:
            raise UsageSummaryNotAvailableError(billing_account_id)

        sub = await self.subscription_repo.get_subscription_by_billing_account_id(
            billing_account_id
        )
        price = (
            await self.plan_price_repo.get_plan_price(sub.plan_price_id)
            if sub and sub.plan_price_id and sub.stripe_status in GOOD_STANDING_STATUSES
            else None
        )
        period = bucket.period_start or _NO_PERIOD
        spent = await self.top_up_repo.spend_cents(billing_account_id, period)
        alerts = await self.usage_alert_repo.list_for_period(billing_account_id, period)

        top_up_block = price.top_up_block_bytes if price else 0
        top_up_price = price.top_up_price_cents if price else 0
        paid = bucket.stripe_managed_period
        used = max(bucket.network_traffic_allowance - bucket.network_traffic_balance, 0)

        return UsageSummary(
            billing_account_id=billing_account_id,
            tier=sub.tier if sub else SubscriptionTier.FREE,
            paid=paid,
            period_start=bucket.period_start,
            period_end=bucket.period_end,
            allowance_bytes=bucket.network_traffic_allowance,
            used_bytes=used,
            remaining_bytes=bucket.network_traffic_balance,
            percent_used=round(percent_used(bucket), 2),
            grace_policy_bytes=bucket.grace_policy_bytes,
            grace_remaining_bytes=bucket.grace_balance,
            grace_active=bucket.network_traffic_balance == 0
            and bucket.grace_balance > 0,
            blocked=bucket.network_traffic_balance == 0 and bucket.grace_balance == 0,
            connection_slot_limit=bucket.connection_slot_limit,
            connection_slots_remaining=bucket.connection_slot_balance,
            auto_top_up_enabled=account.auto_top_up_enabled,
            monthly_top_up_cap_cents=account.monthly_top_up_cap_cents,
            top_up_spent_cents=spent,
            top_up_block_bytes=top_up_block,
            top_up_price_cents=top_up_price,
            can_top_up=top_up_block > 0 and bool(account.stripe_customer_id),
            alerts=alerts,
        )
