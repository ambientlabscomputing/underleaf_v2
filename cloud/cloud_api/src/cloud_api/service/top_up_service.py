import asyncio
from datetime import UTC, datetime

import stripe

from cloud_api import logger
from cloud_api.config import StripeConfig
from cloud_api.models.api import (
    BillingAccount,
    PlanPrice,
    TopUp,
    TopUpSource,
    TopUpStatus,
    UsageAlertKind,
)
from cloud_api.repository.billing_account_repository import BillingAccountRepository
from cloud_api.repository.plan_price_repository import PlanPriceRepository
from cloud_api.repository.subscription_repository import SubscriptionRepository
from cloud_api.repository.top_up_repository import TopUpRepository
from cloud_api.repository.usage_alert_repository import UsageAlertRepository
from cloud_api.service.entitlements_bucket_service import EntitlementsBucketService

# Same sentinel the bucket service uses for legacy buckets with no period.
_NO_PERIOD = datetime(1970, 1, 1, tzinfo=UTC)

TOP_UP_INVOICE_KIND = "data_top_up"

# Stripe subscription states in which the customer is in good standing.
# canceled / past_due / unpaid / incomplete must not be able to buy data.
GOOD_STANDING_STATUSES = {"active", "trialing"}


class TopUpNotAvailableError(Exception):
    """The account can't buy top-ups (free plan, or no payment relationship)."""


class TopUpInProgressError(Exception):
    """A top-up is already awaiting payment; don't double-charge."""


class TopUpPaymentError(Exception):
    """Stripe couldn't take the payment."""


class TopUpService:
    """Prepaid data blocks (e.g. +100 GB for $5) on paid plans.

    Credit is applied only when Stripe reports the invoice *paid* (see
    credit_paid_top_up) -- never optimistically -- and the pending->paid
    transition is atomic, so a replayed webhook can't credit twice.
    """

    def __init__(
        self,
        billing_account_repo: BillingAccountRepository,
        subscription_repo: SubscriptionRepository,
        plan_price_repo: PlanPriceRepository,
        top_up_repo: TopUpRepository,
        usage_alert_repo: UsageAlertRepository,
        entitlements_bucket_service: EntitlementsBucketService,
        stripe_config: StripeConfig,
    ) -> None:
        self.billing_account_repo = billing_account_repo
        self.subscription_repo = subscription_repo
        self.plan_price_repo = plan_price_repo
        self.top_up_repo = top_up_repo
        self.usage_alert_repo = usage_alert_repo
        self.bucket_service = entitlements_bucket_service
        self.stripe_config = stripe_config

    async def _plan_price_for(self, billing_account_id: str) -> PlanPrice | None:
        sub = await self.subscription_repo.get_subscription_by_billing_account_id(
            billing_account_id
        )
        if (
            sub is None
            or sub.stripe_subscription_id is None
            or not sub.plan_price_id
            or sub.stripe_status not in GOOD_STANDING_STATUSES
        ):
            return None
        price = await self.plan_price_repo.get_plan_price(sub.plan_price_id)
        if price is None or price.top_up_block_bytes <= 0:
            return None
        return price

    async def spent_this_period_cents(self, billing_account_id: str) -> int:
        bucket = await self.bucket_service.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        period = (bucket.period_start if bucket else None) or _NO_PERIOD
        return await self.top_up_repo.spend_cents(billing_account_id, period)

    async def try_auto_top_up(self, billing_account_id: str) -> TopUp | None:
        """Run by the overage policy when the allowance is exhausted. Never
        raises: an automatic purchase failing must not break metering."""
        account = await self.billing_account_repo.get_billing_account(
            billing_account_id
        )
        if account is None or not account.auto_top_up_enabled:
            return None
        try:
            return await self._purchase(account, TopUpSource.AUTO)
        except (TopUpNotAvailableError, TopUpInProgressError, TopUpPaymentError) as exc:
            logger.bind(billing_account_id=billing_account_id).info(
                f"Auto top-up not performed: {exc}"
            )
            return None

    async def purchase_manual(self, billing_account_id: str) -> TopUp:
        account = await self.billing_account_repo.get_billing_account(
            billing_account_id
        )
        if account is None:
            raise TopUpNotAvailableError("billing account not found")
        top_up = await self._purchase(account, TopUpSource.MANUAL)
        assert top_up is not None  # manual purchases ignore the auto-spend cap
        return top_up

    async def _purchase(
        self, account: BillingAccount, source: TopUpSource
    ) -> TopUp | None:
        price = await self._plan_price_for(account.id)
        if price is None or not account.stripe_customer_id:
            raise TopUpNotAvailableError("top-ups require an active paid plan")
        if not self.stripe_config.api_key:
            raise TopUpNotAvailableError("billing is not configured")
        if await self.top_up_repo.has_pending(account.id):
            raise TopUpInProgressError("a top-up is already awaiting payment")

        bucket = await self.bucket_service.get_entitlements_bucket_by_billing_account(
            account.id
        )
        period = (bucket.period_start if bucket else None) or _NO_PERIOD

        if source is TopUpSource.AUTO:
            spent = await self.top_up_repo.spend_cents(account.id, period)
            if spent + price.top_up_price_cents > account.monthly_top_up_cap_cents:
                # Cap stops *automatic* purchases only; the grace allowance
                # policy still applies and a manual purchase is still allowed.
                await self.usage_alert_repo.record_if_new(
                    account.id, UsageAlertKind.TOP_UP_CAP_REACHED, period
                )
                raise TopUpNotAvailableError("monthly top-up spending cap reached")

        top_up = await self.top_up_repo.create(
            account.id,
            price.top_up_block_bytes,
            price.top_up_price_cents,
            source,
            period,
        )
        try:
            invoice_id = await asyncio.to_thread(
                self._charge_via_stripe, account, price, top_up
            )
        except stripe.StripeError as exc:
            reason = getattr(exc, "user_message", None) or str(exc)
            await self.top_up_repo.transition(
                top_up.id, TopUpStatus.FAILED, failure_reason=reason
            )
            await self.usage_alert_repo.record_if_new(
                account.id, UsageAlertKind.TOP_UP_FAILED, period
            )
            raise TopUpPaymentError(reason) from exc
        await self.top_up_repo.attach_invoice(top_up.id, invoice_id)
        logger.bind(billing_account_id=account.id, top_up_id=top_up.id).info(
            "Top-up invoice charged; awaiting invoice.paid to credit"
        )
        return top_up

    def _charge_via_stripe(
        self, account: BillingAccount, price: PlanPrice, top_up: TopUp
    ) -> str:
        """Blocking Stripe calls (run in a thread). Returns the invoice id.

        A dedicated one-off invoice rather than a pending invoice item, so the
        charge can never be swept into the next subscription renewal invoice.
        """
        key = self.stripe_config.api_key
        invoice = stripe.Invoice.create(
            customer=account.stripe_customer_id,
            collection_method="charge_automatically",
            auto_advance=False,
            pending_invoice_items_behavior="exclude",
            metadata={
                "kind": TOP_UP_INVOICE_KIND,
                "top_up_id": top_up.id,
                "billing_account_id": account.id,
                "bytes": str(top_up.bytes),
            },
            api_key=key,
            idempotency_key=f"{top_up.id}-invoice",
        )
        try:
            stripe.InvoiceItem.create(
                customer=account.stripe_customer_id,
                invoice=invoice.id,
                amount=top_up.amount_cents,
                currency=price.currency,
                description=f"Data top-up: {top_up.bytes // 10**9} GB",
                api_key=key,
                idempotency_key=f"{top_up.id}-item",
            )
            stripe.Invoice.finalize_invoice(invoice.id, api_key=key)
            stripe.Invoice.pay(invoice.id, api_key=key)
        except stripe.StripeError:
            # Don't leave an open invoice Stripe could try to collect later.
            try:
                stripe.Invoice.void_invoice(invoice.id, api_key=key)
            except stripe.StripeError:
                pass
            raise
        return invoice.id

    async def credit_paid_top_up(self, top_up_id: str, stripe_invoice_id: str) -> bool:
        """Apply a paid top-up. Returns True only for the call that actually
        credited (replays and unknown ids are no-ops)."""
        top_up = await self.top_up_repo.transition(top_up_id, TopUpStatus.PAID)
        if top_up is None:
            return False
        await self.top_up_repo.attach_invoice(top_up.id, stripe_invoice_id)
        await self.bucket_service.credit_top_up(top_up.billing_account_id, top_up.bytes)
        logger.bind(
            billing_account_id=top_up.billing_account_id, top_up_id=top_up.id
        ).info("Top-up credited")
        return True

    async def fail_top_up(self, top_up_id: str, reason: str) -> None:
        top_up = await self.top_up_repo.transition(
            top_up_id, TopUpStatus.FAILED, failure_reason=reason
        )
        if top_up is not None:
            await self.usage_alert_repo.record_if_new(
                top_up.billing_account_id,
                UsageAlertKind.TOP_UP_FAILED,
                top_up.period_start,
            )
