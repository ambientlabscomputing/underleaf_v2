from datetime import UTC, datetime

from sqlalchemy import select, update

from cloud_api.models.api import (
    CreatePlanRequest,
    PatchPlanRequest,
    Plan,
    QueryPlanRequest,
)
from cloud_api.models.sql import SQLPlan
from cloud_api.repository.base_repository import BaseRepository


class PlanRepository(BaseRepository):
    async def create_plan(self, req: CreatePlanRequest) -> Plan:
        async with self.get_session() as session:
            new_plan = SQLPlan(
                name=req.name,
                stripe_product_id=req.stripe_product_id,
                is_active=req.is_active,
                created_at=datetime.now(UTC),
                updated_at=datetime.now(UTC),
            )
            session.add(new_plan)
            await session.commit()
            await session.refresh(new_plan)
            return Plan.model_validate(new_plan)

    async def get_plan(self, plan_id: str) -> Plan | None:
        async with self.get_session() as session:
            plan = await session.get(SQLPlan, plan_id)
            if plan is None:
                return None
            return Plan.model_validate(plan)

    async def query_plans(self, req: QueryPlanRequest) -> list[Plan]:
        async with self.get_session() as session:
            query = select(SQLPlan)
            if req.name is not None:
                query = query.filter(SQLPlan.name == req.name)
            if req.is_active is not None:
                query = query.filter(SQLPlan.is_active == req.is_active)
            result = await session.scalars(query)
            plans = result.all()
            return [Plan.model_validate(plan) for plan in plans]

    async def patch_plan(self, plan_id: str, req: PatchPlanRequest) -> Plan | None:
        async with self.get_session() as session:
            update_stmt = (
                update(SQLPlan)
                .where(SQLPlan.id == plan_id)
                .values(updated_at=datetime.now(UTC))
            )
            if req.name is not None:
                update_stmt = update_stmt.values(name=req.name)
            if req.stripe_product_id is not None:
                update_stmt = update_stmt.values(
                    stripe_product_id=req.stripe_product_id
                )
            if req.is_active is not None:
                update_stmt = update_stmt.values(is_active=req.is_active)
            result = await session.execute(update_stmt)
            if result.rowcount == 0:
                return None
            await session.commit()
            updated_plan = await session.get(SQLPlan, plan_id)
            return Plan.model_validate(updated_plan)

    async def delete_plan(self, plan_id: str) -> bool:
        async with self.get_session() as session:
            plan = await session.get(SQLPlan, plan_id)
            if plan is None:
                return False
            await session.delete(plan)
            await session.commit()
            return True
