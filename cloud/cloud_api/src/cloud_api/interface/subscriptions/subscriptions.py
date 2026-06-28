from fastapi import APIRouter, Depends, HTTPException, status

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    CreateSubscriptionRequest,
    ListSubscriptionResponse,
    PatchSubscriptionRequest,
    QuerySubscriptionRequest,
    Subscription,
    SubscriptionTier,
)
from cloud_api.service.subscription_service import (
    SubscriptionNotFoundError,
    SubscriptionService,
)
from cloud_api.service_manager import get_subscription_service

router = APIRouter(prefix="/subscriptions", tags=["Subscriptions"])


@router.post(
    "",
    response_model=Subscription,
    status_code=status.HTTP_201_CREATED,
    summary="Create a subscription",
)
async def create_subscription(
    req: CreateSubscriptionRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    subscription_service: SubscriptionService = Depends(get_subscription_service),
) -> Subscription:
    return await subscription_service.create_subscription(req)


@router.get(
    "",
    response_model=ListSubscriptionResponse,
    summary="List subscriptions",
)
async def get_subscriptions(
    billing_account_id: str | None = None,
    tier: SubscriptionTier | None = None,
    claims: AccessTokenClaims = Depends(get_access_claims),
    subscription_service: SubscriptionService = Depends(get_subscription_service),
) -> ListSubscriptionResponse:
    return await subscription_service.get_subscriptions(
        QuerySubscriptionRequest(
            billing_account_id=billing_account_id,
            tier=tier,
            principal_account_id=claims.azp,
        )
    )


@router.get(
    "/{subscription_id}",
    response_model=Subscription,
    summary="Get a subscription by ID",
)
async def get_subscription(
    subscription_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    subscription_service: SubscriptionService = Depends(get_subscription_service),
) -> Subscription:
    try:
        return await subscription_service.get_subscription(subscription_id)
    except SubscriptionNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Subscription not found"
        )


@router.patch(
    "/{subscription_id}",
    response_model=Subscription,
    summary="Update a subscription",
)
async def patch_subscription(
    subscription_id: str,
    req: PatchSubscriptionRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    subscription_service: SubscriptionService = Depends(get_subscription_service),
) -> Subscription:
    try:
        return await subscription_service.patch_subscription(subscription_id, req)
    except SubscriptionNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Subscription not found"
        )


@router.delete(
    "/{subscription_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    summary="Delete a subscription",
)
async def delete_subscription(
    subscription_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    subscription_service: SubscriptionService = Depends(get_subscription_service),
) -> None:
    try:
        await subscription_service.delete_subscription(subscription_id)
    except SubscriptionNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Subscription not found"
        )
