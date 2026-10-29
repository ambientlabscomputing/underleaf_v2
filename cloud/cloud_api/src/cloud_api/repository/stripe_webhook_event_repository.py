from datetime import UTC, datetime

from sqlalchemy import select, update
from sqlalchemy.dialects.postgresql import insert as pg_insert

from cloud_api.models.sql import SQLStripeWebhookEvent
from cloud_api.repository.base_repository import BaseRepository


class StripeWebhookEventRepository(BaseRepository):
    async def record_if_new(
        self, stripe_event_id: str, event_type: str, payload: dict
    ) -> bool:
        """Insert a row for this Stripe event id if one doesn't already
        exist. Returns True if this call inserted it (caller should process
        the event), False if it already existed (a retried delivery --
        caller should no-op). The uniqueness check happens at the DB level
        (ON CONFLICT DO NOTHING), not via a prior SELECT, so it's safe
        against concurrent deliveries of the same event."""
        now = datetime.now(UTC)
        async with self.get_session() as session:
            stmt = (
                pg_insert(SQLStripeWebhookEvent)
                .values(
                    stripe_event_id=stripe_event_id,
                    event_type=event_type,
                    payload=payload,
                    created_at=now,
                    updated_at=now,
                )
                .on_conflict_do_nothing(
                    index_elements=[SQLStripeWebhookEvent.stripe_event_id]
                )
                .returning(SQLStripeWebhookEvent.id)
            )
            result = await session.execute(stmt)
            await session.commit()
            return result.first() is not None

    async def needs_reprocessing(
        self, stripe_event_id: str, stale_after_seconds: int = 300
    ) -> bool:
        """For an event we've seen before: should this delivery be handled?

        Yes if the earlier attempt failed (Stripe's retry is how we recover),
        or if it never finished and is old enough to presume the process
        died. No if it succeeded or is plausibly still being handled."""
        async with self.get_session() as session:
            row = (
                await session.scalars(
                    select(SQLStripeWebhookEvent).where(
                        SQLStripeWebhookEvent.stripe_event_id == stripe_event_id
                    )
                )
            ).first()
            if row is None:
                return True
            if row.processing_error is not None:
                return True
            if row.processed_at is None:
                age = datetime.now(UTC) - row.updated_at
                return age.total_seconds() > stale_after_seconds
            return False

    async def mark_processed(
        self, stripe_event_id: str, error: str | None = None
    ) -> None:
        async with self.get_session() as session:
            await session.execute(
                update(SQLStripeWebhookEvent)
                .where(SQLStripeWebhookEvent.stripe_event_id == stripe_event_id)
                .values(
                    processed_at=datetime.now(UTC),
                    processing_error=error,
                    updated_at=datetime.now(UTC),
                )
            )
            await session.commit()
