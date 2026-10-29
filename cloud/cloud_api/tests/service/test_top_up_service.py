"""Failure/eligibility paths for TopUpService with mocked collaborators. The
happy path, cap, pending-guard and credit-once behaviours run against a real
DB in tests/integration/test_overage_policy.py."""

from unittest.mock import AsyncMock, patch

import pytest
import stripe

from cloud_api.config import StripeConfig
from cloud_api.models.api import (
    BillingAccount,
    EntitlementsBucket,
    PlanPrice,
    Subscription,
    TopUp,
    TopUpSource,
    UsageAlertKind,
)
from cloud_api.service.top_up_service import (
    TopUpInProgressError,
    TopUpNotAvailableError,
    TopUpPaymentError,
    TopUpService,
)


def _account(**kw):
    d = {
        "id": "billing_1",
        "name": "A",
        "principal_account_id": "p1",
        "stripe_customer_id": "cus_1",
    }
    d.update(kw)
    return BillingAccount(**d)


def _price():
    return PlanPrice(
        id="price_1",
        plan_id="plan_1",
        interval="month",
        unit_amount_cents=2400,
        network_traffic_limit_bytes=1,
        connection_slot_limit=1,
        grace_bytes=1,
        top_up_block_bytes=100 * 10**9,
        top_up_price_cents=500,
    )


def _service(status="active", api_key="sk_test", with_sub=True):
    deps = {
        "billing_account_repo": AsyncMock(),
        "subscription_repo": AsyncMock(),
        "plan_price_repo": AsyncMock(),
        "top_up_repo": AsyncMock(),
        "usage_alert_repo": AsyncMock(),
        "entitlements_bucket_service": AsyncMock(),
        "stripe_config": StripeConfig(api_key=api_key),
    }
    deps["billing_account_repo"].get_billing_account.return_value = _account()
    deps[
        "subscription_repo"
    ].get_subscription_by_billing_account_id.return_value = Subscription(
        id="s",
        billing_account_id="billing_1",
        tier="builder",
        plan_price_id="price_1" if with_sub else None,
        stripe_subscription_id="sub_1" if with_sub else None,
        stripe_status=status,
    )
    deps["plan_price_repo"].get_plan_price.return_value = _price()
    deps["top_up_repo"].has_pending.return_value = False
    deps["top_up_repo"].spend_cents.return_value = 0
    deps[
        "entitlements_bucket_service"
    ].get_entitlements_bucket_by_billing_account.return_value = EntitlementsBucket(
        id="e", billing_account_id="billing_1", name="b"
    )
    deps["top_up_repo"].create.return_value = TopUp(
        id="topup_1",
        billing_account_id="billing_1",
        bytes=100 * 10**9,
        amount_cents=500,
        source=TopUpSource.MANUAL,
        period_start="2026-10-01T00:00:00Z",
    )
    return TopUpService(**deps), deps


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "status", ["canceled", "past_due", "unpaid", "incomplete", None]
)
async def test_only_good_standing_subscriptions_can_buy(status):
    svc, _ = _service(status=status)
    with pytest.raises(TopUpNotAvailableError):
        await svc.purchase_manual("billing_1")


@pytest.mark.asyncio
async def test_free_account_cannot_buy():
    svc, _ = _service(with_sub=False)
    with pytest.raises(TopUpNotAvailableError):
        await svc.purchase_manual("billing_1")


@pytest.mark.asyncio
async def test_unconfigured_billing_cannot_buy_even_on_a_paid_plan():
    svc, _ = _service(api_key="")
    with pytest.raises(TopUpNotAvailableError, match="not configured"):
        await svc.purchase_manual("billing_1")


@pytest.mark.asyncio
async def test_pending_top_up_blocks_a_second_purchase():
    svc, deps = _service()
    deps["top_up_repo"].has_pending.return_value = True
    with pytest.raises(TopUpInProgressError):
        await svc.purchase_manual("billing_1")
    deps["top_up_repo"].create.assert_not_awaited()


@pytest.mark.asyncio
async def test_card_decline_marks_the_top_up_failed_and_alerts():
    svc, deps = _service()
    with (
        patch.object(
            TopUpService,
            "_charge_via_stripe",
            side_effect=stripe.CardError("declined", "p", "card_declined"),
        ),
        pytest.raises(TopUpPaymentError),
    ):
        await svc.purchase_manual("billing_1")

    assert deps["top_up_repo"].transition.await_args.args[1].value == "failed"
    assert (
        deps["usage_alert_repo"].record_if_new.await_args.args[1]
        == UsageAlertKind.TOP_UP_FAILED
    )


@pytest.mark.asyncio
async def test_auto_purchase_never_raises_even_when_payment_fails():
    svc, deps = _service()
    deps["billing_account_repo"].get_billing_account.return_value = _account(
        auto_top_up_enabled=True
    )
    with patch.object(
        TopUpService,
        "_charge_via_stripe",
        side_effect=stripe.CardError("declined", "p", "card_declined"),
    ):
        assert await svc.try_auto_top_up("billing_1") is None


@pytest.mark.asyncio
async def test_auto_top_up_off_does_nothing():
    svc, deps = _service()
    assert await svc.try_auto_top_up("billing_1") is None
    deps["top_up_repo"].create.assert_not_awaited()
