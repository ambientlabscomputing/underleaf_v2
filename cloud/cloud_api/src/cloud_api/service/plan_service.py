from cloud_api import logger
from cloud_api.models.api import (
    CreatePlanPriceRequest,
    CreatePlanRequest,
    ListPlanPriceResponse,
    ListPlanResponse,
    PatchPlanPriceRequest,
    PatchPlanRequest,
    Plan,
    PlanPrice,
    QueryPlanPriceRequest,
    QueryPlanRequest,
)
from cloud_api.repository.plan_price_repository import PlanPriceRepository
from cloud_api.repository.plan_repository import PlanRepository


class PlanNotFoundError(Exception):
    def __init__(self, plan_id: str) -> None:
        super().__init__(f"Plan '{plan_id}' not found")
        self.plan_id = plan_id


class PlanPriceNotFoundError(Exception):
    def __init__(self, plan_price_id: str) -> None:
        super().__init__(f"PlanPrice '{plan_price_id}' not found")
        self.plan_price_id = plan_price_id


class PlanService:
    def __init__(
        self, plan_repo: PlanRepository, plan_price_repo: PlanPriceRepository
    ) -> None:
        self.plan_repo = plan_repo
        self.plan_price_repo = plan_price_repo

    async def create_plan(self, req: CreatePlanRequest) -> Plan:
        plan = await self.plan_repo.create_plan(req)
        logger.bind(plan_id=plan.id).info("Plan created")
        return plan

    async def get_plans(self, req: QueryPlanRequest) -> ListPlanResponse:
        items = await self.plan_repo.query_plans(req)
        return ListPlanResponse(items=items, total=len(items))

    async def get_plan(self, plan_id: str) -> Plan:
        plan = await self.plan_repo.get_plan(plan_id)
        if plan is None:
            raise PlanNotFoundError(plan_id)
        return plan

    async def patch_plan(self, plan_id: str, req: PatchPlanRequest) -> Plan:
        plan = await self.plan_repo.patch_plan(plan_id, req)
        if plan is None:
            raise PlanNotFoundError(plan_id)
        return plan

    async def delete_plan(self, plan_id: str) -> None:
        deleted = await self.plan_repo.delete_plan(plan_id)
        if not deleted:
            raise PlanNotFoundError(plan_id)
        logger.bind(plan_id=plan_id).info("Plan deleted")

    async def create_plan_price(self, req: CreatePlanPriceRequest) -> PlanPrice:
        price = await self.plan_price_repo.create_plan_price(req)
        logger.bind(plan_price_id=price.id, plan_id=price.plan_id).info(
            "PlanPrice created"
        )
        return price

    async def get_plan_prices(
        self, req: QueryPlanPriceRequest
    ) -> ListPlanPriceResponse:
        items = await self.plan_price_repo.query_plan_prices(req)
        return ListPlanPriceResponse(items=items, total=len(items))

    async def get_plan_price(self, plan_price_id: str) -> PlanPrice:
        price = await self.plan_price_repo.get_plan_price(plan_price_id)
        if price is None:
            raise PlanPriceNotFoundError(plan_price_id)
        return price

    async def get_plan_price_by_stripe_price_id(
        self, stripe_price_id: str
    ) -> PlanPrice | None:
        return await self.plan_price_repo.get_plan_price_by_stripe_price_id(
            stripe_price_id
        )

    async def patch_plan_price(
        self, plan_price_id: str, req: PatchPlanPriceRequest
    ) -> PlanPrice:
        price = await self.plan_price_repo.patch_plan_price(plan_price_id, req)
        if price is None:
            raise PlanPriceNotFoundError(plan_price_id)
        return price

    async def delete_plan_price(self, plan_price_id: str) -> None:
        deleted = await self.plan_price_repo.delete_plan_price(plan_price_id)
        if not deleted:
            raise PlanPriceNotFoundError(plan_price_id)
        logger.bind(plan_price_id=plan_price_id).info("PlanPrice deleted")
