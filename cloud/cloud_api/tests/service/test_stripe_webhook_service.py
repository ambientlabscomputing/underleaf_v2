"""Branch-level unit tests for StripeWebhookService with mocked collaborators.

The behaviours that matter end to end (renewal refill, top-up credit,
plan-change usage preservation, replay safety) are covered against a real
database in tests/integration/test_overage_policy.py; these cover the guard
rails around them.
"""

from unittest.mock import AsyncMock

import pytest

from cloud_api.models.api import BillingAccount
from cloud_api.service.stripe_webhook_service import StripeWebhookService
from tests.helpers import stripe_event


def _make_service():
    deps = {
        "webhook_event_repo": AsyncMock(),
        "subscription_repo": AsyncMock(),
        "billing_account_repo": AsyncMock(),
        "plan_repo": AsyncMock(),
        "plan_price_repo": AsyncMock(),
        "entitlements_bucket_service": AsyncMock(),
        "invoice_repo": AsyncMock(),
        "top_up_service": AsyncMock(),
    }
    deps["webhook_event_repo"].record_if_new.return_value = True
    return StripeWebhookService(**deps), deps


@pytest.mark.asyncio
async def test_successfully_processed_duplicate_is_skipped():
    service, deps = _make_service()
    deps["webhook_event_repo"].record_if_new.return_value = False
    deps["webhook_event_repo"].needs_reprocessing.return_value = False

    await service.handle_event(stripe_event("evt_1", "checkout.session.completed", {}))

    deps["webhook_event_repo"].mark_processed.assert_not_awaited()
    deps["subscription_repo"].sync_from_stripe.assert_not_awaited()


@pytest.mark.asyncio
async def test_previously_failed_event_is_reprocessed_on_retry():
    service, deps = _make_service()
    deps["webhook_event_repo"].record_if_new.return_value = False
    deps["webhook_event_repo"].needs_reprocessing.return_value = True

    await service.handle_event(stripe_event("evt_1", "some.unhandled.event", {}))

    deps["webhook_event_repo"].mark_processed.assert_awaited_once_with("evt_1", None)


@pytest.mark.asyncio
async def test_unknown_event_type_is_recorded_without_error():
    service, deps = _make_service()

    await service.handle_event(stripe_event("evt_1", "some.unhandled.event", {}))

    deps["webhook_event_repo"].mark_processed.assert_awaited_once_with("evt_1", None)


@pytest.mark.asyncio
async def test_checkout_missing_metadata_is_skipped():
    service, deps = _make_service()

    await service.handle_event(
        stripe_event(
            "evt_1",
            "checkout.session.completed",
            {"metadata": {}, "subscription": None},
        )
    )

    deps["subscription_repo"].sync_from_stripe.assert_not_awaited()
    deps["entitlements_bucket_service"].apply_plan.assert_not_awaited()


@pytest.mark.asyncio
async def test_invoice_for_unknown_customer_is_skipped():
    service, deps = _make_service()
    deps[
        "billing_account_repo"
    ].get_billing_account_by_stripe_customer_id.return_value = None

    await service.handle_event(
        stripe_event("evt_1", "invoice.paid", {"id": "in_1", "customer": "cus_x"})
    )

    deps["invoice_repo"].upsert_invoice.assert_not_awaited()
    deps["top_up_service"].credit_paid_top_up.assert_not_awaited()


@pytest.mark.asyncio
async def test_top_up_invoice_credits_via_top_up_service_not_renewal():
    service, deps = _make_service()
    deps[
        "billing_account_repo"
    ].get_billing_account_by_stripe_customer_id.return_value = BillingAccount(
        id="billing_1", name="A", principal_account_id="p1"
    )

    await service.handle_event(
        stripe_event(
            "evt_1",
            "invoice.paid",
            {
                "id": "in_1",
                "customer": "cus_x",
                "billing_reason": "manual",
                "metadata": {"kind": "data_top_up", "top_up_id": "topup_1"},
            },
        )
    )

    deps["top_up_service"].credit_paid_top_up.assert_awaited_once_with(
        "topup_1", "in_1"
    )
    deps["entitlements_bucket_service"].apply_plan.assert_not_awaited()


@pytest.mark.asyncio
async def test_handler_error_is_recorded_and_reraised():
    service, deps = _make_service()
    deps[
        "subscription_repo"
    ].get_subscription_by_billing_account_id.side_effect = RuntimeError("boom")

    with pytest.raises(RuntimeError):
        await service.handle_event(
            stripe_event(
                "evt_1",
                "checkout.session.completed",
                {
                    "metadata": {"billing_account_id": "b", "plan_price_id": "p"},
                    "subscription": "sub_1",
                },
            )
        )

    deps["webhook_event_repo"].mark_processed.assert_awaited_once_with("evt_1", "boom")
