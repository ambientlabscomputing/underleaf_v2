from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

import pytest

from cloud_api.config import StripeConfig
from cloud_api.models.api import Plan, PlanPrice
from cloud_api.service.stripe_catalog_sync import StripeCatalogSync


def _plan(name, product_id=None):
    return Plan(id=f"plan_{name}", name=name, stripe_product_id=product_id)


def _price(name, cents, stripe_price_id=None):
    return PlanPrice(
        id=f"price_{name}",
        plan_id=f"plan_{name}",
        stripe_price_id=stripe_price_id,
        interval="month",
        unit_amount_cents=cents,
        network_traffic_limit_bytes=1,
        connection_slot_limit=1,
    )


def _sync(plans, prices_by_plan, api_key="sk_test_x"):
    plan_repo, price_repo = AsyncMock(), AsyncMock()
    plan_repo.query_plans.return_value = plans
    price_repo.query_plan_prices.side_effect = lambda req: prices_by_plan[req.plan_id]
    return (
        StripeCatalogSync(plan_repo, price_repo, StripeConfig(api_key=api_key)),
        plan_repo,
        price_repo,
    )


@pytest.mark.asyncio
async def test_requires_an_api_key():
    sync, _, _ = _sync([], {}, api_key="")
    with pytest.raises(RuntimeError, match="API key"):
        await sync.run()


@pytest.mark.asyncio
async def test_creates_product_and_price_for_paid_plans_and_skips_free():
    sync, plan_repo, price_repo = _sync(
        [_plan("free"), _plan("maker")],
        {"plan_free": [_price("free", 0)], "plan_maker": [_price("maker", 900)]},
    )
    with (
        patch(
            "cloud_api.service.stripe_catalog_sync.stripe.Product.create",
            return_value=SimpleNamespace(id="prod_maker"),
        ) as product,
        patch(
            "cloud_api.service.stripe_catalog_sync.stripe.Price.list",
            return_value=SimpleNamespace(data=[]),
        ),
        patch(
            "cloud_api.service.stripe_catalog_sync.stripe.Price.create",
            return_value=SimpleNamespace(id="price_stripe_maker"),
        ) as price,
    ):
        linked = await sync.run()

    assert linked == {"maker": "price_stripe_maker"}
    assert product.call_count == 1  # free was never sent to Stripe
    assert price.call_args.kwargs["unit_amount"] == 900
    assert price.call_args.kwargs["recurring"] == {"interval": "month"}
    assert price.call_args.kwargs["lookup_key"] == "underleaf_maker_month"
    assert plan_repo.patch_plan.await_args.args[1].stripe_product_id == "prod_maker"
    assert (
        price_repo.patch_plan_price.await_args.args[1].stripe_price_id
        == "price_stripe_maker"
    )


@pytest.mark.asyncio
async def test_already_linked_plans_are_not_recreated():
    sync, _, price_repo = _sync(
        [_plan("maker", product_id="prod_x")],
        {"plan_maker": [_price("maker", 900, stripe_price_id="price_x")]},
    )
    with (
        patch("cloud_api.service.stripe_catalog_sync.stripe.Product.create") as product,
        patch("cloud_api.service.stripe_catalog_sync.stripe.Price.create") as price,
    ):
        linked = await sync.run()

    assert linked == {"maker": "price_x"}
    product.assert_not_called()
    price.assert_not_called()
    price_repo.patch_plan_price.assert_not_awaited()


@pytest.mark.asyncio
async def test_adopts_existing_stripe_price_and_its_product_without_creating_anything():
    """Objects made by the CLI script must be adopted, not duplicated -- in
    particular the Product, which has no lookup key of its own."""
    sync, plan_repo, price_repo = _sync(
        [_plan("maker")], {"plan_maker": [_price("maker", 900)]}
    )
    with (
        patch(
            "cloud_api.service.stripe_catalog_sync.stripe.Price.list",
            return_value=SimpleNamespace(
                data=[SimpleNamespace(id="price_existing", product="prod_existing")]
            ),
        ),
        patch(
            "cloud_api.service.stripe_catalog_sync.stripe.Price.create"
        ) as price_create,
        patch(
            "cloud_api.service.stripe_catalog_sync.stripe.Product.create"
        ) as product_create,
    ):
        linked = await sync.run()

    assert linked == {"maker": "price_existing"}
    price_create.assert_not_called()
    product_create.assert_not_called()
    assert plan_repo.patch_plan.await_args.args[1].stripe_product_id == "prod_existing"
    assert (
        price_repo.patch_plan_price.await_args.args[1].stripe_price_id
        == "price_existing"
    )
