from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

import pytest

from cloud_api.config import StripeConfig
from cloud_api.models.api import BillingAccount, PlanPrice
from cloud_api.service.billing_account_service import BillingAccountNotFoundError
from cloud_api.service.plan_service import PlanPriceNotFoundError
from cloud_api.service.stripe_checkout_service import (
    PlanPriceNotSellableError,
    StripeCheckoutService,
)


def _make_billing_account(**overrides) -> BillingAccount:
    defaults = {
        "id": "billing_1",
        "name": "Acme",
        "principal_account_id": "principal_1",
        "stripe_customer_id": None,
    }
    defaults.update(overrides)
    return BillingAccount(**defaults)


def _make_plan_price(**overrides) -> PlanPrice:
    defaults = {
        "id": "price_1",
        "plan_id": "plan_1",
        "stripe_price_id": "price_stripe_123",
        "interval": "month",
        "unit_amount_cents": 1000,
        "network_traffic_limit_bytes": 1_000_000,
        "connection_slot_limit": 5,
    }
    defaults.update(overrides)
    return PlanPrice(**defaults)


def _make_service(**overrides):
    deps = {
        "billing_account_repo": AsyncMock(),
        "plan_price_repo": AsyncMock(),
        "stripe_config": StripeConfig(api_key="sk_test_123"),
    }
    deps.update(overrides)
    return StripeCheckoutService(**deps), deps


@pytest.mark.asyncio
async def test_create_checkout_session_billing_account_not_found():
    service, deps = _make_service()
    deps["billing_account_repo"].get_billing_account.return_value = None

    with pytest.raises(BillingAccountNotFoundError):
        await service.create_checkout_session("billing_missing", "price_1")


@pytest.mark.asyncio
async def test_create_checkout_session_plan_price_not_found():
    service, deps = _make_service()
    deps[
        "billing_account_repo"
    ].get_billing_account.return_value = _make_billing_account()
    deps["plan_price_repo"].get_plan_price.return_value = None

    with pytest.raises(PlanPriceNotFoundError):
        await service.create_checkout_session("billing_1", "price_missing")


@pytest.mark.asyncio
async def test_create_checkout_session_plan_price_not_sellable():
    service, deps = _make_service()
    deps[
        "billing_account_repo"
    ].get_billing_account.return_value = _make_billing_account()
    deps["plan_price_repo"].get_plan_price.return_value = _make_plan_price(
        stripe_price_id=None
    )

    with pytest.raises(PlanPriceNotSellableError):
        await service.create_checkout_session("billing_1", "price_1")


@pytest.mark.asyncio
async def test_create_checkout_session_creates_stripe_customer_lazily():
    service, deps = _make_service()
    deps[
        "billing_account_repo"
    ].get_billing_account.return_value = _make_billing_account(stripe_customer_id=None)
    deps["plan_price_repo"].get_plan_price.return_value = _make_plan_price()

    with (
        patch(
            "cloud_api.service.stripe_checkout_service.stripe.Customer.create",
            return_value=SimpleNamespace(id="cus_new123"),
        ) as mock_create_customer,
        patch(
            "cloud_api.service.stripe_checkout_service.stripe.checkout.Session.create",
            return_value=SimpleNamespace(url="https://checkout.stripe.com/session_x"),
        ) as mock_create_session,
    ):
        result = await service.create_checkout_session("billing_1", "price_1")

    mock_create_customer.assert_called_once()
    deps["billing_account_repo"].patch_billing_account.assert_awaited_once()
    patch_call_args = deps["billing_account_repo"].patch_billing_account.await_args
    assert patch_call_args.args[0] == "billing_1"
    assert patch_call_args.args[1].stripe_customer_id == "cus_new123"

    mock_create_session.assert_called_once()
    _, session_kwargs = mock_create_session.call_args
    assert session_kwargs["customer"] == "cus_new123"
    assert session_kwargs["mode"] == "subscription"
    assert session_kwargs["line_items"] == [
        {"price": "price_stripe_123", "quantity": 1}
    ]
    assert result.url == "https://checkout.stripe.com/session_x"


@pytest.mark.asyncio
async def test_create_checkout_session_reuses_existing_stripe_customer():
    service, deps = _make_service()
    deps[
        "billing_account_repo"
    ].get_billing_account.return_value = _make_billing_account(
        stripe_customer_id="cus_existing"
    )
    deps["plan_price_repo"].get_plan_price.return_value = _make_plan_price()

    with (
        patch(
            "cloud_api.service.stripe_checkout_service.stripe.Customer.create"
        ) as mock_create_customer,
        patch(
            "cloud_api.service.stripe_checkout_service.stripe.checkout.Session.create",
            return_value=SimpleNamespace(url="https://checkout.stripe.com/session_y"),
        ) as mock_create_session,
    ):
        await service.create_checkout_session("billing_1", "price_1")

    mock_create_customer.assert_not_called()
    deps["billing_account_repo"].patch_billing_account.assert_not_awaited()
    _, session_kwargs = mock_create_session.call_args
    assert session_kwargs["customer"] == "cus_existing"
