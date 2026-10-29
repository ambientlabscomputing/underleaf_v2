from cloud_api import logger
from cloud_api.models.api import (
    CreateSubscriptionRequest,
    ListSubscriptionResponse,
    PatchSubscriptionRequest,
    QuerySubscriptionRequest,
    Subscription,
)
from cloud_api.repository.subscription_repository import SubscriptionRepository


class SubscriptionNotFoundError(Exception):
    def __init__(self, subscription_id: str) -> None:
        super().__init__(f"Subscription '{subscription_id}' not found")
        self.subscription_id = subscription_id


class SubscriptionTierManagedByStripeError(Exception):
    """Raised when a client PATCH tries to change the tier of a subscription
    that's already tied to a real Stripe subscription. Once Stripe is in
    the loop, tier is derived from Stripe events (see StripeWebhookService)
    -- accepting a bare client PATCH here would let anyone upgrade
    themselves for free."""

    def __init__(self, subscription_id: str) -> None:
        super().__init__(
            f"Subscription '{subscription_id}' tier is managed by Stripe; "
            "change it via Stripe Checkout / the customer portal instead"
        )
        self.subscription_id = subscription_id


class SubscriptionService:
    def __init__(self, subscription_repo: SubscriptionRepository) -> None:
        self.subscription_repo = subscription_repo

    async def create_subscription(self, req: CreateSubscriptionRequest) -> Subscription:
        subscription = await self.subscription_repo.create_subscription(req)
        logger.bind(subscription_id=subscription.id).info("Subscription created")
        return subscription

    async def get_subscriptions(
        self, req: QuerySubscriptionRequest
    ) -> ListSubscriptionResponse:
        items = await self.subscription_repo.query_subscriptions(req)
        return ListSubscriptionResponse(items=items, total=len(items))

    async def get_subscription(self, subscription_id: str) -> Subscription:
        subscription = await self.subscription_repo.get_subscription(subscription_id)
        if subscription is None:
            raise SubscriptionNotFoundError(subscription_id)
        return subscription

    async def patch_subscription(
        self, subscription_id: str, req: PatchSubscriptionRequest
    ) -> Subscription:
        existing = await self.subscription_repo.get_subscription(subscription_id)
        if existing is None:
            raise SubscriptionNotFoundError(subscription_id)
        if (
            req.tier is not None
            and req.tier != existing.tier
            and existing.stripe_subscription_id is not None
        ):
            raise SubscriptionTierManagedByStripeError(subscription_id)
        subscription = await self.subscription_repo.patch_subscription(
            subscription_id, req
        )
        if subscription is None:
            raise SubscriptionNotFoundError(subscription_id)
        return subscription

    async def delete_subscription(self, subscription_id: str) -> None:
        deleted = await self.subscription_repo.delete_subscription(subscription_id)
        if not deleted:
            raise SubscriptionNotFoundError(subscription_id)
        logger.bind(subscription_id=subscription_id).info("Subscription deleted")
