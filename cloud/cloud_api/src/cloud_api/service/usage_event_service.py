from datetime import datetime

from cloud_api import logger
from cloud_api.models.api import (
    CreateUsageEventRequest,
    ListUsageEventResponse,
    QueryUsageEventRequest,
    UsageEvent,
    UsageEventType,
    UsageUnits,
)
from cloud_api.repository.usage_event_repository import UsageEventRepository


class UsageEventNotFoundError(Exception):
    def __init__(self, usage_event_id: str) -> None:
        super().__init__(f"UsageEvent '{usage_event_id}' not found")
        self.usage_event_id = usage_event_id


class UsageEventService:
    def __init__(self, usage_event_repo: UsageEventRepository) -> None:
        self.usage_event_repo = usage_event_repo

    async def create_usage_event(self, req: CreateUsageEventRequest) -> UsageEvent:
        event = await self.usage_event_repo.create_usage_event(req)
        logger.bind(
            usage_event_id=event.id, billing_account_id=event.billing_account_id
        ).info("UsageEvent recorded")
        return event

    async def record_usage(
        self,
        billing_account_id: str,
        event_type: UsageEventType,
        usage_unit: UsageUnits,
        usage_amount: int,
        period_start: datetime,
        period_end: datetime,
        idempotency_key: str,
        stream_id: str | None = None,
    ) -> tuple[UsageEvent, bool]:
        """Record usage for internal callers (e.g. the sync loop). Returns
        (event, created); created is False when the idempotency key was seen
        before, which callers use to avoid repeating side effects."""
        event, created = await self.usage_event_repo.create_usage_event_idempotent(
            CreateUsageEventRequest(
                billing_account_id=billing_account_id,
                event_type=event_type,
                usage_unit=usage_unit,
                usage_amount=usage_amount,
                stream_id=stream_id,
                period_start=period_start,
                period_end=period_end,
                idempotency_key=idempotency_key,
            )
        )
        if created:
            logger.bind(
                usage_event_id=event.id, billing_account_id=event.billing_account_id
            ).info("UsageEvent recorded")
        return event, created

    async def get_usage_events(
        self, req: QueryUsageEventRequest
    ) -> ListUsageEventResponse:
        items = await self.usage_event_repo.query_usage_events(req)
        return ListUsageEventResponse(items=items, total=len(items))

    async def get_usage_event(self, usage_event_id: str) -> UsageEvent:
        event = await self.usage_event_repo.get_usage_event(usage_event_id)
        if event is None:
            raise UsageEventNotFoundError(usage_event_id)
        return event

    async def delete_usage_event(self, usage_event_id: str) -> None:
        deleted = await self.usage_event_repo.delete_usage_event(usage_event_id)
        if not deleted:
            raise UsageEventNotFoundError(usage_event_id)
        logger.bind(usage_event_id=usage_event_id).info("UsageEvent deleted")
