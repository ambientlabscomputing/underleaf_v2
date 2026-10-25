from datetime import UTC, datetime

from sqlalchemy import func, select, update

from cloud_api.models.api import TopUp, TopUpSource, TopUpStatus
from cloud_api.models.sql import SQLTopUp
from cloud_api.repository.base_repository import BaseRepository


class TopUpRepository(BaseRepository):
    async def create(
        self,
        billing_account_id: str,
        bytes_: int,
        amount_cents: int,
        source: TopUpSource,
        period_start: datetime,
    ) -> TopUp:
        now = datetime.now(UTC)
        async with self.get_session() as session:
            row = SQLTopUp(
                billing_account_id=billing_account_id,
                bytes=bytes_,
                amount_cents=amount_cents,
                source=source.value,
                status=TopUpStatus.PENDING.value,
                period_start=period_start,
                created_at=now,
                updated_at=now,
            )
            session.add(row)
            await session.commit()
            await session.refresh(row)
            return TopUp.model_validate(row)

    async def get(self, top_up_id: str) -> TopUp | None:
        async with self.get_session() as session:
            row = await session.get(SQLTopUp, top_up_id)
            return TopUp.model_validate(row) if row else None

    async def has_pending(self, billing_account_id: str) -> bool:
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLTopUp.id)
                .where(SQLTopUp.billing_account_id == billing_account_id)
                .where(SQLTopUp.status == TopUpStatus.PENDING.value)
                .limit(1)
            )
            return result.first() is not None

    async def spend_cents(self, billing_account_id: str, period_start: datetime) -> int:
        """Automatic + manual spend this period, counting pending purchases so
        a slow payment can't be double-booked against the cap."""
        async with self.get_session() as session:
            result = await session.execute(
                select(func.coalesce(func.sum(SQLTopUp.amount_cents), 0))
                .where(SQLTopUp.billing_account_id == billing_account_id)
                .where(SQLTopUp.period_start == period_start)
                .where(
                    SQLTopUp.status.in_(
                        [TopUpStatus.PENDING.value, TopUpStatus.PAID.value]
                    )
                )
            )
            return int(result.scalar_one())

    async def attach_invoice(self, top_up_id: str, stripe_invoice_id: str) -> None:
        async with self.get_session() as session:
            await session.execute(
                update(SQLTopUp)
                .where(SQLTopUp.id == top_up_id)
                .values(
                    stripe_invoice_id=stripe_invoice_id,
                    updated_at=datetime.now(UTC),
                )
            )
            await session.commit()

    async def transition(
        self,
        top_up_id: str,
        to: TopUpStatus,
        *,
        failure_reason: str | None = None,
    ) -> TopUp | None:
        """pending -> paid|failed, exactly once. Returns the updated row only
        for the caller that performed the transition, which is what makes the
        webhook credit idempotent (a replayed invoice.paid gets None)."""
        async with self.get_session() as session:
            result = await session.execute(
                update(SQLTopUp)
                .where(SQLTopUp.id == top_up_id)
                .where(SQLTopUp.status == TopUpStatus.PENDING.value)
                .values(
                    status=to.value,
                    failure_reason=failure_reason,
                    updated_at=datetime.now(UTC),
                )
                .returning(SQLTopUp)
            )
            row = result.scalar_one_or_none()
            if row is None:
                return None
            await session.commit()
            return TopUp.model_validate(row)
