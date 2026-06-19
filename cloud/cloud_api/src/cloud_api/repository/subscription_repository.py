from cloud_api.models.api import (
    Subscription,
    CreateSubscriptionRequest,
    QuerySubscriptionRequest,
    PatchSubscriptionRequest,
)
from cloud_api.models.sql import SQLSubscription
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update
from datetime import datetime, timezone


class SubscriptionRepository(BaseRepository):
    async def create_subscription(self, req: CreateSubscriptionRequest) -> Subscription:
        async with self.get_session() as session:
            new_subscription = SQLSubscription(
                billing_account_id=req.billing_account_id,
                tier=req.tier,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
            )
            session.add(new_subscription)
            await session.commit()
            await session.refresh(new_subscription)
            return Subscription.model_validate(new_subscription)

    async def get_subscription(self, subscription_id: str) -> Subscription | None:
        async with self.get_session() as session:
            subscription = await session.get(SQLSubscription, subscription_id)
            if subscription is None:
                return None
            return Subscription.model_validate(subscription)

    async def query_subscriptions(
        self, req: QuerySubscriptionRequest
    ) -> list[Subscription]:
        async with self.get_session() as session:
            query = select(SQLSubscription)
            if req.billing_account_id is not None:
                query = query.filter(
                    SQLSubscription.billing_account_id == req.billing_account_id
                )
            if req.tier is not None:
                query = query.filter(SQLSubscription.tier == req.tier)
            result = await session.scalars(query)
            subscriptions = result.all()
            return [
                Subscription.model_validate(subscription)
                for subscription in subscriptions
            ]

    async def patch_subscription(
        self, subscription_id: str, req: PatchSubscriptionRequest
    ) -> Subscription | None:
        async with self.get_session() as session:
            update_stmt = update(SQLSubscription).where(
                SQLSubscription.id == subscription_id
            ).values(updated_at=datetime.now(timezone.utc))
            if req.tier is not None:
                update_stmt = update_stmt.values(tier=req.tier)
            result = await session.execute(update_stmt)
            if result.scalar() is None:
                return None
            await session.commit()
            updated_subscription = await session.get(SQLSubscription, subscription_id)
            return Subscription.model_validate(updated_subscription)

    async def delete_subscription(self, subscription_id: str) -> bool:
        async with self.get_session() as session:
            subscription = await session.get(SQLSubscription, subscription_id)
            if subscription is None:
                return False
            await session.delete(subscription)
            await session.commit()
            return True
