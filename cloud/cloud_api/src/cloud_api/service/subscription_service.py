from cloud_api.models.api import (
    Subscription,
    CreateSubscriptionRequest,
    ListSubscriptionResponse,
    PatchSubscriptionRequest,
    QuerySubscriptionRequest,
)
from cloud_api.repository.subscription_repository import SubscriptionRepository
from cloud_api import logger


class SubscriptionNotFoundError(Exception):
    def __init__(self, subscription_id: str) -> None:
        super().__init__(f"Subscription '{subscription_id}' not found")
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
