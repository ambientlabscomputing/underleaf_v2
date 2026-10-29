from datetime import UTC, datetime

from sqlalchemy import select
from sqlalchemy.dialects.postgresql import insert as pg_insert

from cloud_api.models.api import UsageAlert, UsageAlertKind
from cloud_api.models.sql import SQLUsageAlert
from cloud_api.repository.base_repository import BaseRepository


class UsageAlertRepository(BaseRepository):
    async def record_if_new(
        self,
        billing_account_id: str,
        kind: UsageAlertKind,
        period_start: datetime,
    ) -> bool:
        """True only for the call that created the alert, so each threshold
        notifies exactly once per allowance period even under concurrency
        (unique constraint + ON CONFLICT DO NOTHING)."""
        now = datetime.now(UTC)
        async with self.get_session() as session:
            result = await session.execute(
                pg_insert(SQLUsageAlert)
                .values(
                    billing_account_id=billing_account_id,
                    kind=kind.value,
                    period_start=period_start,
                    created_at=now,
                    updated_at=now,
                )
                .on_conflict_do_nothing(constraint="uq_usage_alerts_once")
                .returning(SQLUsageAlert.id)
            )
            await session.commit()
            return result.first() is not None

    async def list_for_period(
        self, billing_account_id: str, period_start: datetime
    ) -> list[UsageAlert]:
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLUsageAlert)
                .where(SQLUsageAlert.billing_account_id == billing_account_id)
                .where(SQLUsageAlert.period_start == period_start)
                .order_by(SQLUsageAlert.created_at)
            )
            return [UsageAlert.model_validate(a) for a in result.all()]
