from datetime import UTC, datetime

from cloud_api import logger
from cloud_api.lib import catalog
from cloud_api.models.api import (
    PlanPrice,
    StripeSyncSubscriptionRequest,
    SubscriptionTier,
    UpsertInvoiceRequest,
)
from cloud_api.repository.billing_account_repository import BillingAccountRepository
from cloud_api.repository.invoice_repository import InvoiceRepository
from cloud_api.repository.plan_price_repository import PlanPriceRepository
from cloud_api.repository.plan_repository import PlanRepository
from cloud_api.repository.stripe_webhook_event_repository import (
    StripeWebhookEventRepository,
)
from cloud_api.repository.subscription_repository import SubscriptionRepository
from cloud_api.service.entitlements_bucket_service import EntitlementsBucketService
from cloud_api.service.top_up_service import TOP_UP_INVOICE_KIND, TopUpService


def _unix_to_datetime(ts: int | None) -> datetime | None:
    if ts is None:
        return None
    return datetime.fromtimestamp(ts, tz=UTC)


def _invoice_subscription_id(invoice: dict) -> str | None:
    """Stripe moved the subscription reference on invoices in newer API
    versions; accept both shapes."""
    if invoice.get("subscription"):
        return invoice["subscription"]
    parent = invoice.get("parent") or {}
    details = parent.get("subscription_details") or {}
    return details.get("subscription")


def _invoice_period(invoice: dict) -> tuple[datetime | None, datetime | None]:
    lines = (invoice.get("lines") or {}).get("data") or []
    if lines and lines[0].get("period"):
        period = lines[0]["period"]
        return _unix_to_datetime(period.get("start")), _unix_to_datetime(
            period.get("end")
        )
    return None, None


class StripeWebhookService:
    """Processes already signature-verified Stripe events (verification lives
    in interface/stripe_webhooks/stripe_webhooks.py).

    Delivery is at-least-once, so every handler is safe to replay: events are
    de-duplicated by Stripe's event id (but a *failed* attempt is retried, not
    swallowed), and the effects themselves are guarded -- renewals only refill
    when the period actually advanced, top-up credit only on the atomic
    pending->paid transition.
    """

    def __init__(
        self,
        webhook_event_repo: StripeWebhookEventRepository,
        subscription_repo: SubscriptionRepository,
        billing_account_repo: BillingAccountRepository,
        plan_repo: PlanRepository,
        plan_price_repo: PlanPriceRepository,
        entitlements_bucket_service: EntitlementsBucketService,
        invoice_repo: InvoiceRepository,
        top_up_service: TopUpService,
    ) -> None:
        self.webhook_event_repo = webhook_event_repo
        self.subscription_repo = subscription_repo
        self.billing_account_repo = billing_account_repo
        self.plan_repo = plan_repo
        self.plan_price_repo = plan_price_repo
        self.bucket_service = entitlements_bucket_service
        self.invoice_repo = invoice_repo
        self.top_up_service = top_up_service
        self._handlers = {
            "checkout.session.completed": self._handle_checkout_completed,
            "customer.subscription.updated": self._handle_subscription_updated,
            "customer.subscription.deleted": self._handle_subscription_deleted,
            "invoice.paid": self._handle_invoice_paid,
            "invoice.payment_failed": self._handle_invoice_payment_failed,
        }

    async def handle_event(self, event) -> None:
        # Handlers read events with dict methods (.get), which stripe-python's
        # StripeObject stopped supporting -- normalize to plain dicts once here.
        event = event.to_dict(for_json=True)
        is_new = await self.webhook_event_repo.record_if_new(
            event["id"], event["type"], event
        )
        if not is_new and not await self.webhook_event_repo.needs_reprocessing(
            event["id"]
        ):
            logger.bind(stripe_event_id=event["id"], event_type=event["type"]).info(
                "Duplicate Stripe webhook delivery, skipping"
            )
            return

        handler = self._handlers.get(event["type"])
        error: str | None = None
        try:
            if handler is not None:
                await handler(event)
            else:
                logger.bind(event_type=event["type"]).debug(
                    "No handler for this Stripe event type, ignoring"
                )
        except Exception as exc:
            error = str(exc) or type(exc).__name__
            raise
        finally:
            await self.webhook_event_repo.mark_processed(event["id"], error)

    # -- helpers ---------------------------------------------------------------

    async def _apply_plan_price(
        self,
        billing_account_id: str,
        plan_price: PlanPrice,
        *,
        reset_usage: bool,
        period_start: datetime | None = None,
        period_end: datetime | None = None,
    ) -> None:
        await self.bucket_service.apply_plan(
            billing_account_id,
            allowance=plan_price.network_traffic_limit_bytes,
            slot_limit=plan_price.connection_slot_limit,
            grace_policy_bytes=plan_price.grace_bytes,
            stripe_managed=True,
            reset_usage=reset_usage,
            period_start=period_start,
            period_end=period_end,
        )

    async def _tier_for_plan_price(
        self, plan_price: PlanPrice
    ) -> SubscriptionTier | None:
        plan = await self.plan_repo.get_plan(plan_price.plan_id)
        return _tier_for_plan_name(plan.name if plan else None)

    # -- handlers --------------------------------------------------------------

    async def _handle_checkout_completed(self, event) -> None:
        session = event["data"]["object"]
        metadata = session.get("metadata") or {}
        billing_account_id = metadata.get("billing_account_id")
        plan_price_id = metadata.get("plan_price_id")
        stripe_subscription_id = session.get("subscription")
        if not billing_account_id or not plan_price_id:
            logger.bind(stripe_event_id=event["id"]).warning(
                "checkout.session.completed missing billing_account_id/plan_price_id "
                "metadata; skipping"
            )
            return

        subscription = (
            await self.subscription_repo.get_subscription_by_billing_account_id(
                billing_account_id
            )
        )
        if subscription is None:
            logger.bind(billing_account_id=billing_account_id).warning(
                "No local subscription for billing account; skipping checkout sync"
            )
            return
        plan_price = await self.plan_price_repo.get_plan_price(plan_price_id)
        if plan_price is None:
            logger.bind(plan_price_id=plan_price_id).warning(
                "Unknown plan_price_id in checkout session metadata; skipping"
            )
            return

        already_active = (
            stripe_subscription_id is not None
            and subscription.stripe_subscription_id == stripe_subscription_id
        )
        await self.subscription_repo.sync_from_stripe(
            subscription.id,
            StripeSyncSubscriptionRequest(
                tier=await self._tier_for_plan_price(plan_price),
                plan_price_id=plan_price.id,
                stripe_subscription_id=stripe_subscription_id,
                stripe_status="active",
            ),
        )
        if not already_active:
            # New paid period starts now (renewals take over from invoice.paid).
            await self._apply_plan_price(
                billing_account_id,
                plan_price,
                reset_usage=True,
                period_start=datetime.now(UTC),
            )
        logger.bind(
            billing_account_id=billing_account_id, plan_price_id=plan_price_id
        ).info("Subscription activated from Stripe checkout")

    async def _handle_subscription_updated(self, event) -> None:
        stripe_sub = event["data"]["object"]
        subscription = (
            await self.subscription_repo.get_subscription_by_stripe_subscription_id(
                stripe_sub["id"]
            )
        )
        if subscription is None:
            logger.bind(stripe_subscription_id=stripe_sub["id"]).warning(
                "customer.subscription.updated for unknown local subscription"
            )
            return

        sync_req = StripeSyncSubscriptionRequest(
            stripe_status=stripe_sub.get("status"),
            current_period_start=_unix_to_datetime(
                stripe_sub.get("current_period_start")
            ),
            current_period_end=_unix_to_datetime(stripe_sub.get("current_period_end")),
            cancel_at_period_end=stripe_sub.get("cancel_at_period_end", False),
        )

        items = (stripe_sub.get("items") or {}).get("data") or []
        if items:
            plan_price = await self.plan_price_repo.get_plan_price_by_stripe_price_id(
                items[0]["price"]["id"]
            )
            if plan_price is not None and plan_price.id != subscription.plan_price_id:
                sync_req = sync_req.model_copy(
                    update={
                        "plan_price_id": plan_price.id,
                        "tier": await self._tier_for_plan_price(plan_price),
                    }
                )
                # Mid-period plan change: keep what's already been used.
                await self._apply_plan_price(
                    subscription.billing_account_id, plan_price, reset_usage=False
                )

        await self.subscription_repo.sync_from_stripe(subscription.id, sync_req)
        logger.bind(
            subscription_id=subscription.id, stripe_status=stripe_sub.get("status")
        ).info("Subscription synced from Stripe")

    async def _handle_subscription_deleted(self, event) -> None:
        stripe_sub = event["data"]["object"]
        subscription = (
            await self.subscription_repo.get_subscription_by_stripe_subscription_id(
                stripe_sub["id"]
            )
        )
        if subscription is None:
            logger.bind(stripe_subscription_id=stripe_sub["id"]).warning(
                "customer.subscription.deleted for unknown local subscription"
            )
            return

        await self.subscription_repo.sync_from_stripe(
            subscription.id,
            StripeSyncSubscriptionRequest(
                tier=SubscriptionTier.FREE,
                stripe_status="canceled",
                cancel_at_period_end=False,
            ),
        )
        start, end = catalog.month_bounds()
        await self.bucket_service.apply_plan(
            subscription.billing_account_id,
            allowance=catalog.FREE.included_bytes,
            slot_limit=catalog.FREE.connection_slots,
            grace_policy_bytes=catalog.FREE.grace_bytes,
            stripe_managed=False,
            reset_usage=True,
            period_start=start,
            period_end=end,
        )
        logger.bind(subscription_id=subscription.id).info(
            "Subscription cancelled, downgraded to FREE"
        )

    async def _upsert_invoice(self, invoice: dict) -> bool:
        customer_id = invoice.get("customer")
        if not customer_id:
            return False
        billing_account = (
            await self.billing_account_repo.get_billing_account_by_stripe_customer_id(
                customer_id
            )
        )
        if billing_account is None:
            logger.bind(stripe_customer_id=customer_id).warning(
                "Invoice event for unknown billing account; skipping"
            )
            return False
        await self.invoice_repo.upsert_invoice(
            UpsertInvoiceRequest(
                billing_account_id=billing_account.id,
                stripe_invoice_id=invoice["id"],
                status=invoice.get("status") or "unknown",
                amount_due_cents=invoice.get("amount_due", 0),
                amount_paid_cents=invoice.get("amount_paid", 0),
                currency=invoice.get("currency", "usd"),
                hosted_invoice_url=invoice.get("hosted_invoice_url"),
                invoice_pdf_url=invoice.get("invoice_pdf"),
            )
        )
        return True

    async def _handle_invoice_paid(self, event) -> None:
        invoice = event["data"]["object"]
        if not await self._upsert_invoice(invoice):
            return
        metadata = invoice.get("metadata") or {}

        if metadata.get("kind") == TOP_UP_INVOICE_KIND:
            if metadata.get("top_up_id"):
                await self.top_up_service.credit_paid_top_up(
                    metadata["top_up_id"], invoice["id"]
                )
            return

        # A paid renewal is what refills the allowance -- never the calendar,
        # so a customer whose payment fails isn't granted a new month of data.
        if invoice.get("billing_reason") == "subscription_cycle":
            await self._renew_allowance(invoice)

    async def _renew_allowance(self, invoice: dict) -> None:
        stripe_subscription_id = _invoice_subscription_id(invoice)
        if not stripe_subscription_id:
            return
        subscription = (
            await self.subscription_repo.get_subscription_by_stripe_subscription_id(
                stripe_subscription_id
            )
        )
        if subscription is None or not subscription.plan_price_id:
            logger.bind(stripe_subscription_id=stripe_subscription_id).warning(
                "Renewal invoice for unknown subscription; allowance not refilled"
            )
            return
        plan_price = await self.plan_price_repo.get_plan_price(
            subscription.plan_price_id
        )
        if plan_price is None:
            return

        period_start, period_end = _invoice_period(invoice)
        period_start = period_start or datetime.now(UTC)

        bucket = await self.bucket_service.get_entitlements_bucket_by_billing_account(
            subscription.billing_account_id
        )
        if bucket is not None and bucket.period_start == period_start:
            return  # replayed delivery: this period was already refilled

        await self._apply_plan_price(
            subscription.billing_account_id,
            plan_price,
            reset_usage=True,
            period_start=period_start,
            period_end=period_end,
        )
        logger.bind(billing_account_id=subscription.billing_account_id).info(
            "Allowance refilled for new billing period"
        )

    async def _handle_invoice_payment_failed(self, event) -> None:
        invoice = event["data"]["object"]
        await self._upsert_invoice(invoice)
        metadata = invoice.get("metadata") or {}
        if metadata.get("kind") == TOP_UP_INVOICE_KIND and metadata.get("top_up_id"):
            await self.top_up_service.fail_top_up(
                metadata["top_up_id"], "Payment for the data top-up failed"
            )


def _tier_for_plan_name(plan_name: str | None) -> SubscriptionTier | None:
    if plan_name is None:
        return None
    try:
        return SubscriptionTier(plan_name.lower())
    except ValueError:
        # Plan name outside the display enum: leave tier untouched rather
        # than failing the whole event over a cosmetic column.
        logger.bind(plan_name=plan_name).warning(
            "Plan name has no matching SubscriptionTier; tier left unchanged"
        )
        return None
