import asyncio

import stripe

from cloud_api import logger
from cloud_api.config import StripeConfig
from cloud_api.models.api import (
    BillingAccount,
    CreateCheckoutSessionResponse,
    InternalPatchBillingAccountRequest,
)
from cloud_api.repository.billing_account_repository import BillingAccountRepository
from cloud_api.repository.plan_price_repository import PlanPriceRepository
from cloud_api.service.billing_account_service import BillingAccountNotFoundError
from cloud_api.service.plan_service import PlanPriceNotFoundError


class StripeNotConfiguredError(Exception):
    """No Stripe API key is configured, so nothing can be sold or charged."""


class PlanPriceNotSellableError(Exception):
    """Raised when a plan_price has no stripe_price_id -- it exists in our
    catalog but was never (or is no longer) linked to a real Stripe Price,
    so there's nothing to check out."""

    def __init__(self, plan_price_id: str) -> None:
        super().__init__(f"PlanPrice '{plan_price_id}' has no stripe_price_id")
        self.plan_price_id = plan_price_id


class StripeCheckoutService:
    def __init__(
        self,
        billing_account_repo: BillingAccountRepository,
        plan_price_repo: PlanPriceRepository,
        stripe_config: StripeConfig,
    ) -> None:
        self.billing_account_repo = billing_account_repo
        self.plan_price_repo = plan_price_repo
        self.stripe_config = stripe_config

    async def _get_or_create_stripe_customer_id(
        self, billing_account: BillingAccount
    ) -> str:
        # Lazy on first checkout, not at signup -- avoids creating a Stripe
        # customer object for every signup when most never convert off FREE.
        if billing_account.stripe_customer_id:
            return billing_account.stripe_customer_id
        customer = await asyncio.to_thread(
            stripe.Customer.create,
            name=billing_account.name,
            metadata={
                "billing_account_id": billing_account.id,
                "principal_account_id": billing_account.principal_account_id,
            },
            api_key=self.stripe_config.api_key,
        )
        await self.billing_account_repo.patch_billing_account(
            billing_account.id,
            InternalPatchBillingAccountRequest(stripe_customer_id=customer.id),
        )
        logger.bind(
            billing_account_id=billing_account.id, stripe_customer_id=customer.id
        ).info("Stripe customer created")
        return customer.id

    async def create_checkout_session(
        self, billing_account_id: str, plan_price_id: str
    ) -> CreateCheckoutSessionResponse:
        if not self.stripe_config.api_key:
            raise StripeNotConfiguredError()
        billing_account = await self.billing_account_repo.get_billing_account(
            billing_account_id
        )
        if billing_account is None:
            raise BillingAccountNotFoundError(billing_account_id)
        plan_price = await self.plan_price_repo.get_plan_price(plan_price_id)
        if plan_price is None:
            raise PlanPriceNotFoundError(plan_price_id)
        if not plan_price.stripe_price_id:
            raise PlanPriceNotSellableError(plan_price_id)

        customer_id = await self._get_or_create_stripe_customer_id(billing_account)
        session = await asyncio.to_thread(
            stripe.checkout.Session.create,
            mode="subscription",
            customer=customer_id,
            line_items=[{"price": plan_price.stripe_price_id, "quantity": 1}],
            success_url=self.stripe_config.success_url,
            cancel_url=self.stripe_config.cancel_url,
            metadata={
                "billing_account_id": billing_account.id,
                "plan_price_id": plan_price.id,
            },
            api_key=self.stripe_config.api_key,
        )
        logger.bind(
            billing_account_id=billing_account_id, plan_price_id=plan_price_id
        ).info("Stripe checkout session created")
        return CreateCheckoutSessionResponse(url=session.url)
