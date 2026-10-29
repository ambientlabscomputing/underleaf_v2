from fastapi import APIRouter, Depends, HTTPException, status

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    ListPlanResponse,
    Plan,
    QueryPlanRequest,
)
from cloud_api.service.plan_service import PlanNotFoundError, PlanService
from cloud_api.service_manager import get_plan_service

# Read-only: the catalog is managed by migration / sync_stripe_catalog. A
# customer-writable catalog would let anyone attach a cheap Stripe price to
# a plan with a huge allowance.
router = APIRouter(prefix="/plans", tags=["Plans"])


@router.get(
    "",
    response_model=ListPlanResponse,
    summary="List plans (the plan-picker catalog)",
)
async def get_plans(
    name: str | None = None,
    is_active: bool | None = None,
    claims: AccessTokenClaims = Depends(get_access_claims),
    plan_service: PlanService = Depends(get_plan_service),
) -> ListPlanResponse:
    return await plan_service.get_plans(
        QueryPlanRequest(name=name, is_active=is_active)
    )


@router.get(
    "/{plan_id}",
    response_model=Plan,
    summary="Get a plan by ID",
)
async def get_plan(
    plan_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    plan_service: PlanService = Depends(get_plan_service),
) -> Plan:
    try:
        return await plan_service.get_plan(plan_id)
    except PlanNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Plan not found"
        )
