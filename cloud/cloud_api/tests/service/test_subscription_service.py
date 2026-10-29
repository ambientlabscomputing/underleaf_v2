from unittest.mock import AsyncMock

import pytest

from cloud_api.models.api import (
    PatchSubscriptionRequest,
    Subscription,
    SubscriptionTier,
)
from cloud_api.service.subscription_service import (
    SubscriptionService,
    SubscriptionTierManagedByStripeError,
)


def _make_subscription(**overrides) -> Subscription:
    defaults = {
        "id": "subscription_1",
        "billing_account_id": "billing_1",
        "tier": SubscriptionTier.FREE,
        "stripe_subscription_id": None,
    }
    defaults.update(overrides)
    return Subscription(**defaults)


@pytest.mark.asyncio
async def test_patch_subscription_blocks_tier_change_once_stripe_managed():
    repo = AsyncMock()
    repo.get_subscription.return_value = _make_subscription(
        tier=SubscriptionTier.BUILDER, stripe_subscription_id="sub_123"
    )
    service = SubscriptionService(subscription_repo=repo)

    with pytest.raises(SubscriptionTierManagedByStripeError):
        await service.patch_subscription(
            "subscription_1", PatchSubscriptionRequest(tier=SubscriptionTier.FREE)
        )

    repo.patch_subscription.assert_not_awaited()


@pytest.mark.asyncio
async def test_patch_subscription_allows_tier_change_when_not_stripe_managed():
    repo = AsyncMock()
    repo.get_subscription.return_value = _make_subscription(
        tier=SubscriptionTier.FREE, stripe_subscription_id=None
    )
    repo.patch_subscription.return_value = _make_subscription(
        tier=SubscriptionTier.BUILDER
    )
    service = SubscriptionService(subscription_repo=repo)

    result = await service.patch_subscription(
        "subscription_1", PatchSubscriptionRequest(tier=SubscriptionTier.BUILDER)
    )

    assert result.tier == SubscriptionTier.BUILDER
    repo.patch_subscription.assert_awaited_once()


@pytest.mark.asyncio
async def test_patch_subscription_allows_no_op_tier_even_when_stripe_managed():
    """Setting tier to its current value isn't a real change, so it
    shouldn't trip the guard (e.g. a PATCH that only touches other future
    fields but still echoes the current tier)."""
    repo = AsyncMock()
    repo.get_subscription.return_value = _make_subscription(
        tier=SubscriptionTier.BUILDER, stripe_subscription_id="sub_123"
    )
    repo.patch_subscription.return_value = _make_subscription(
        tier=SubscriptionTier.BUILDER, stripe_subscription_id="sub_123"
    )
    service = SubscriptionService(subscription_repo=repo)

    await service.patch_subscription(
        "subscription_1", PatchSubscriptionRequest(tier=SubscriptionTier.BUILDER)
    )

    repo.patch_subscription.assert_awaited_once()
