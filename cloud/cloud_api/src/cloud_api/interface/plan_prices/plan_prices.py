from fastapi import APIRouter, Depends, HTTPException, status

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    ListPlanPriceResponse,
    PlanPrice,
    QueryPlanPriceRequest,
)
from cloud_api.service.plan_service import PlanPriceNotFoundError, PlanService
from cloud_api.service_manager import get_plan_service

# Read-only -- see plans.py.
router = APIRouter(prefix="/plan-prices", tags=["Plan Prices"])


@router.get(
    "",
    response_model=ListPlanPriceResponse,
    summary="List plan prices",
)
async def get_plan_prices(
    plan_id: str | None = None,
    is_active: bool | None = None,
    claims: AccessTokenClaims = Depends(get_access_claims),
    plan_service: PlanService = Depends(get_plan_service),
) -> ListPlanPriceResponse:
    return await plan_service.get_plan_prices(
        QueryPlanPriceRequest(plan_id=plan_id, is_active=is_active)
    )


@router.get(
    "/{plan_price_id}",
    response_model=PlanPrice,
    summary="Get a plan price by ID",
)
async def get_plan_price(
    plan_price_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    plan_service: PlanService = Depends(get_plan_service),
) -> PlanPrice:
    try:
        return await plan_service.get_plan_price(plan_price_id)
    except PlanPriceNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Plan price not found"
        )
