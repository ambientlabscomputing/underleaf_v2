import asyncio

import stripe

from cloud_api import logger
from cloud_api.config import StripeConfig
from cloud_api.models.api import (
    PatchPlanPriceRequest,
    PatchPlanRequest,
    QueryPlanPriceRequest,
    QueryPlanRequest,
)
from cloud_api.repository.plan_price_repository import PlanPriceRepository
from cloud_api.repository.plan_repository import PlanRepository


class StripeCatalogSync:
    """Creates the Stripe Products/Prices for every paid plan in the catalog
    and records their ids, so Checkout has something to sell.

    Idempotent: ids already recorded are left alone, Prices are found by
    lookup_key before being created, and creation calls carry idempotency
    keys, so re-running (or racing two runs) never duplicates objects.
    """

    def __init__(
        self,
        plan_repo: PlanRepository,
        plan_price_repo: PlanPriceRepository,
        stripe_config: StripeConfig,
    ) -> None:
        self.plan_repo = plan_repo
        self.plan_price_repo = plan_price_repo
        self.stripe_config = stripe_config

    async def run(self) -> dict[str, str]:
        """Returns {plan_name: stripe_price_id} for everything now linked."""
        if not self.stripe_config.api_key:
            raise RuntimeError(
                "No Stripe API key configured (set UNDERLEAF_STRIPE_API_KEY)"
            )
        linked: dict[str, str] = {}
        for plan in await self.plan_repo.query_plans(QueryPlanRequest()):
            prices = await self.plan_price_repo.query_plan_prices(
                QueryPlanPriceRequest(plan_id=plan.id)
            )
            for price in prices:
                if price.unit_amount_cents <= 0:
                    continue  # free: never sold through Stripe
                # Adopt objects that already exist in Stripe (e.g. created with
                # `stripe_cli_catalog`) before creating anything, so the two
                # tools can be mixed without duplicating Products.
                existing = (
                    None
                    if price.stripe_price_id
                    else await asyncio.to_thread(self._find_price, plan.name, price)
                )
                product_id = plan.stripe_product_id or (
                    existing.product if existing else None
                )
                if not product_id:
                    product_id = await asyncio.to_thread(
                        self._ensure_product, plan.name
                    )
                if product_id != plan.stripe_product_id:
                    await self.plan_repo.patch_plan(
                        plan.id, PatchPlanRequest(stripe_product_id=product_id)
                    )
                price_id = price.stripe_price_id
                if not price_id:
                    price_id = (
                        existing.id
                        if existing
                        else await asyncio.to_thread(
                            self._create_price, plan.name, product_id, price
                        )
                    )
                    await self.plan_price_repo.patch_plan_price(
                        price.id, PatchPlanPriceRequest(stripe_price_id=price_id)
                    )
                linked[plan.name] = price_id
                logger.bind(plan=plan.name).info("Plan linked to Stripe")
        return linked

    def _ensure_product(self, plan_name: str) -> str:
        product = stripe.Product.create(
            name=f"Underleaf {plan_name.capitalize()}",
            metadata={"underleaf_plan": plan_name},
            api_key=self.stripe_config.api_key,
            idempotency_key=f"underleaf-product-{plan_name}",
        )
        return product.id

    def _lookup_key(self, plan_name: str, price) -> str:
        return f"underleaf_{plan_name}_{price.interval}"

    def _find_price(self, plan_name: str, price):
        found = stripe.Price.list(
            lookup_keys=[self._lookup_key(plan_name, price)],
            active=True,
            api_key=self.stripe_config.api_key,
        )
        return found.data[0] if found.data else None

    def _create_price(self, plan_name: str, product_id: str, price) -> str:
        lookup_key = self._lookup_key(plan_name, price)
        created = stripe.Price.create(
            product=product_id,
            unit_amount=price.unit_amount_cents,
            currency=price.currency,
            recurring={"interval": price.interval},
            lookup_key=lookup_key,
            metadata={"underleaf_plan": plan_name},
            api_key=self.stripe_config.api_key,
            idempotency_key=f"underleaf-price-{lookup_key}-{price.unit_amount_cents}",
        )
        return created.id
