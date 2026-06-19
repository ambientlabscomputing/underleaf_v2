from cloud_api.models.api import (
    UsageEvent,
    CreateUsageEventRequest,
    QueryUsageEventRequest,
)
from cloud_api.models.sql import SQLUsageEvent
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select
from datetime import datetime, timezone


class UsageEventRepository(BaseRepository):
    async def create_usage_event(self, req: CreateUsageEventRequest) -> UsageEvent:
        async with self.get_session() as session:
            new_event = SQLUsageEvent(
                billing_account_id=req.billing_account_id,
                event_type=req.event_type,
                usage_unit=req.usage_unit,
                usage_amount=req.usage_amount,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
            )
            session.add(new_event)
            await session.commit()
            await session.refresh(new_event)
            return UsageEvent.model_validate(new_event)

    async def get_usage_event(self, usage_event_id: str) -> UsageEvent | None:
        async with self.get_session() as session:
            event = await session.get(SQLUsageEvent, usage_event_id)
            if event is None:
                return None
            return UsageEvent.model_validate(event)

    async def query_usage_events(self, req: QueryUsageEventRequest) -> list[UsageEvent]:
        async with self.get_session() as session:
            query = select(SQLUsageEvent)
            if req.billing_account_id is not None:
                query = query.filter(
                    SQLUsageEvent.billing_account_id == req.billing_account_id
                )
            if req.event_type is not None:
                query = query.filter(SQLUsageEvent.event_type == req.event_type)
            if req.usage_unit is not None:
                query = query.filter(SQLUsageEvent.usage_unit == req.usage_unit)
            result = await session.scalars(query)
            events = result.all()
            return [UsageEvent.model_validate(event) for event in events]

    async def delete_usage_event(self, usage_event_id: str) -> bool:
        async with self.get_session() as session:
            event = await session.get(SQLUsageEvent, usage_event_id)
            if event is None:
                return False
            await session.delete(event)
            await session.commit()
            return True
