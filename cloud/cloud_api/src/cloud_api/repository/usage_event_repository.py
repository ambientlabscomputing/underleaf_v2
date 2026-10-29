from datetime import UTC, datetime

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError

from cloud_api.models.api import (
    CreateUsageEventRequest,
    QueryUsageEventRequest,
    UsageEvent,
)
from cloud_api.models.sql import SQLUsageEvent
from cloud_api.repository.base_repository import BaseRepository


class UsageEventRepository(BaseRepository):
    async def create_usage_event(self, req: CreateUsageEventRequest) -> UsageEvent:
        event, _ = await self.create_usage_event_idempotent(req)
        return event

    async def create_usage_event_idempotent(
        self, req: CreateUsageEventRequest
    ) -> tuple[UsageEvent, bool]:
        """Create a usage event, idempotent on `idempotency_key`.

        Returns (event, created). A retried call with the same key returns
        the original row with created=False, so callers can make the work
        that follows (e.g. charging an allowance) happen exactly once.
        """
        async with self.get_session() as session:
            new_event = SQLUsageEvent(
                billing_account_id=req.billing_account_id,
                event_type=req.event_type,
                usage_unit=req.usage_unit,
                usage_amount=req.usage_amount,
                stream_id=req.stream_id,
                period_start=req.period_start,
                period_end=req.period_end,
                idempotency_key=req.idempotency_key,
                created_at=datetime.now(UTC),
                updated_at=datetime.now(UTC),
            )
            session.add(new_event)
            try:
                await session.commit()
            except IntegrityError:
                await session.rollback()
                existing = await self.get_usage_event_by_idempotency_key(
                    req.idempotency_key
                )
                if existing is not None:
                    return existing, False
                raise
            await session.refresh(new_event)
            return UsageEvent.model_validate(new_event), True

    async def get_usage_event(self, usage_event_id: str) -> UsageEvent | None:
        async with self.get_session() as session:
            event = await session.get(SQLUsageEvent, usage_event_id)
            if event is None:
                return None
            return UsageEvent.model_validate(event)

    async def get_usage_event_by_idempotency_key(
        self, idempotency_key: str
    ) -> UsageEvent | None:
        async with self.get_session() as session:
            query = select(SQLUsageEvent).filter(
                SQLUsageEvent.idempotency_key == idempotency_key
            )
            result = await session.scalars(query)
            event = result.first()
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
            if req.stream_id is not None:
                query = query.filter(SQLUsageEvent.stream_id == req.stream_id)
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
