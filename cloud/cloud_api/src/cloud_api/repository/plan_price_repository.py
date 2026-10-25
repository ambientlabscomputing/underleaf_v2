from datetime import UTC, datetime

from sqlalchemy import select, update

from cloud_api.models.api import (
    CreatePlanPriceRequest,
    PatchPlanPriceRequest,
    PlanPrice,
    QueryPlanPriceRequest,
)
from cloud_api.models.sql import SQLPlanPrice
from cloud_api.repository.base_repository import BaseRepository


class PlanPriceRepository(BaseRepository):
    async def create_plan_price(self, req: CreatePlanPriceRequest) -> PlanPrice:
        async with self.get_session() as session:
            new_price = SQLPlanPrice(
                plan_id=req.plan_id,
                stripe_price_id=req.stripe_price_id,
                interval=req.interval,
                unit_amount_cents=req.unit_amount_cents,
                currency=req.currency,
                is_active=req.is_active,
                network_traffic_limit_bytes=req.network_traffic_limit_bytes,
                connection_slot_limit=req.connection_slot_limit,
                grace_bytes=req.grace_bytes,
                top_up_block_bytes=req.top_up_block_bytes,
                top_up_price_cents=req.top_up_price_cents,
                created_at=datetime.now(UTC),
                updated_at=datetime.now(UTC),
            )
            session.add(new_price)
            await session.commit()
            await session.refresh(new_price)
            return PlanPrice.model_validate(new_price)

    async def get_plan_price(self, plan_price_id: str) -> PlanPrice | None:
        async with self.get_session() as session:
            price = await session.get(SQLPlanPrice, plan_price_id)
            if price is None:
                return None
            return PlanPrice.model_validate(price)

    async def get_plan_price_by_stripe_price_id(
        self, stripe_price_id: str
    ) -> PlanPrice | None:
        async with self.get_session() as session:
            query = select(SQLPlanPrice).filter(
                SQLPlanPrice.stripe_price_id == stripe_price_id
            )
            result = await session.scalars(query)
            price = result.first()
            if price is None:
                return None
            return PlanPrice.model_validate(price)

    async def query_plan_prices(self, req: QueryPlanPriceRequest) -> list[PlanPrice]:
        async with self.get_session() as session:
            query = select(SQLPlanPrice)
            if req.plan_id is not None:
                query = query.filter(SQLPlanPrice.plan_id == req.plan_id)
            if req.stripe_price_id is not None:
                query = query.filter(
                    SQLPlanPrice.stripe_price_id == req.stripe_price_id
                )
            if req.is_active is not None:
                query = query.filter(SQLPlanPrice.is_active == req.is_active)
            result = await session.scalars(query)
            prices = result.all()
            return [PlanPrice.model_validate(price) for price in prices]

    async def patch_plan_price(
        self, plan_price_id: str, req: PatchPlanPriceRequest
    ) -> PlanPrice | None:
        async with self.get_session() as session:
            update_stmt = (
                update(SQLPlanPrice)
                .where(SQLPlanPrice.id == plan_price_id)
                .values(updated_at=datetime.now(UTC))
            )
            if req.stripe_price_id is not None:
                update_stmt = update_stmt.values(stripe_price_id=req.stripe_price_id)
            if req.is_active is not None:
                update_stmt = update_stmt.values(is_active=req.is_active)
            if req.unit_amount_cents is not None:
                update_stmt = update_stmt.values(
                    unit_amount_cents=req.unit_amount_cents
                )
            if req.network_traffic_limit_bytes is not None:
                update_stmt = update_stmt.values(
                    network_traffic_limit_bytes=req.network_traffic_limit_bytes
                )
            if req.connection_slot_limit is not None:
                update_stmt = update_stmt.values(
                    connection_slot_limit=req.connection_slot_limit
                )
            result = await session.execute(update_stmt)
            if result.rowcount == 0:
                return None
            await session.commit()
            updated_price = await session.get(SQLPlanPrice, plan_price_id)
            return PlanPrice.model_validate(updated_price)

    async def delete_plan_price(self, plan_price_id: str) -> bool:
        async with self.get_session() as session:
            price = await session.get(SQLPlanPrice, plan_price_id)
            if price is None:
                return False
            await session.delete(price)
            await session.commit()
            return True
