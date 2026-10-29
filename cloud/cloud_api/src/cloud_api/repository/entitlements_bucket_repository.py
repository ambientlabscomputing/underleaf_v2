from datetime import UTC, datetime
from enum import StrEnum

from sqlalchemy import func, select, update

from cloud_api.models.api import (
    CreateEntitlementsBucketRequest,
    EntitlementsBucket,
    PatchEntitlementsBucketRequest,
    QueryEntitlementsBucketRequest,
)
from cloud_api.models.sql import SQLEntitlementsBucket
from cloud_api.repository.base_repository import BaseRepository

B = SQLEntitlementsBucket


class EntitlementsBalanceField(StrEnum):
    NETWORK_TRAFFIC = "network_traffic_balance"
    CONNECTION_SLOT = "connection_slot_balance"


def _now() -> datetime:
    return datetime.now(UTC)


class EntitlementsBucketRepository(BaseRepository):
    async def create_entitlements_bucket(
        self, req: CreateEntitlementsBucketRequest
    ) -> EntitlementsBucket:
        async with self.get_session() as session:
            new_bucket = SQLEntitlementsBucket(
                name=req.name,
                billing_account_id=req.billing_account_id,
                network_traffic_allowance=req.network_traffic_allowance,
                network_traffic_balance=req.network_traffic_allowance,
                connection_slot_limit=req.connection_slot_limit,
                connection_slot_balance=req.connection_slot_limit,
                grace_policy_bytes=req.grace_policy_bytes,
                grace_balance=0,
                period_start=req.period_start,
                period_end=req.period_end,
                stripe_managed_period=req.stripe_managed_period,
                created_at=_now(),
                updated_at=_now(),
            )
            session.add(new_bucket)
            await session.commit()
            await session.refresh(new_bucket)
            return EntitlementsBucket.model_validate(new_bucket)

    async def get_entitlements_bucket(
        self, entitlements_bucket_id: str
    ) -> EntitlementsBucket | None:
        async with self.get_session() as session:
            bucket = await session.get(SQLEntitlementsBucket, entitlements_bucket_id)
            if bucket is None:
                return None
            return EntitlementsBucket.model_validate(bucket)

    async def query_entitlements_buckets(
        self, req: QueryEntitlementsBucketRequest
    ) -> list[EntitlementsBucket]:
        async with self.get_session() as session:
            query = select(SQLEntitlementsBucket)
            if req.name is not None:
                query = query.filter(SQLEntitlementsBucket.name == req.name)
            if req.billing_account_id is not None:
                query = query.filter(
                    SQLEntitlementsBucket.billing_account_id == req.billing_account_id
                )
            result = await session.scalars(query)
            return [EntitlementsBucket.model_validate(b) for b in result.all()]

    async def get_entitlements_bucket_by_billing_account(
        self, billing_account_id: str
    ) -> EntitlementsBucket | None:
        async with self.get_session() as session:
            query = select(SQLEntitlementsBucket).filter(
                SQLEntitlementsBucket.billing_account_id == billing_account_id
            )
            result = await session.scalars(query)
            bucket = result.first()
            if bucket is None:
                return None
            return EntitlementsBucket.model_validate(bucket)

    async def patch_entitlements_bucket(
        self, entitlements_bucket_id: str, req: PatchEntitlementsBucketRequest
    ) -> EntitlementsBucket | None:
        """Raw admin-style write. Customer-facing code must never expose this;
        plan changes go through apply_plan so usage is preserved."""
        async with self.get_session() as session:
            values = req.model_dump(exclude_none=True)
            update_stmt = (
                update(B)
                .where(B.id == entitlements_bucket_id)
                .values(updated_at=_now(), **values)
            )
            result = await session.execute(update_stmt)
            if result.rowcount == 0:
                return None
            await session.commit()
            updated = await session.get(B, entitlements_bucket_id)
            return EntitlementsBucket.model_validate(updated)

    async def decrement_balance(
        self,
        entitlements_bucket_id: str,
        field: EntitlementsBalanceField,
        amount: int,
    ) -> EntitlementsBucket | None:
        """Atomically decrement `field` by `amount`, refusing to go negative.

        One conditional UPDATE (not read-then-write) so concurrent callers
        can't lose an update. Returns None if the bucket doesn't exist or the
        balance is insufficient.
        """
        if amount < 0:
            raise ValueError("amount must be non-negative")
        column = getattr(B, field.value)
        async with self.get_session() as session:
            result = await session.execute(
                update(B)
                .where(B.id == entitlements_bucket_id)
                .where(column >= amount)
                .values(**{field.value: column - amount}, updated_at=_now())
                .returning(B)
            )
            row = result.scalar_one_or_none()
            if row is None:
                return None
            await session.commit()
            return EntitlementsBucket.model_validate(row)

    async def increment_balance(
        self,
        entitlements_bucket_id: str,
        field: EntitlementsBalanceField,
        amount: int,
    ) -> EntitlementsBucket | None:
        """Atomic counterpart to decrement_balance. Connection slots are
        capped at connection_slot_limit so a double release can't mint slots."""
        if amount < 0:
            raise ValueError("amount must be non-negative")
        column = getattr(B, field.value)
        new_value = column + amount
        if field is EntitlementsBalanceField.CONNECTION_SLOT:
            new_value = func.least(new_value, B.connection_slot_limit)
        async with self.get_session() as session:
            result = await session.execute(
                update(B)
                .where(B.id == entitlements_bucket_id)
                .values(**{field.value: new_value}, updated_at=_now())
                .returning(B)
            )
            row = result.scalar_one_or_none()
            if row is None:
                return None
            await session.commit()
            return EntitlementsBucket.model_validate(row)

    async def set_slot_balance(
        self, entitlements_bucket_id: str, open_connections: int
    ) -> EntitlementsBucket | None:
        """Reconcile slots to ground truth: limit minus connections actually
        open. Heals leaks from connections that closed without a release."""
        async with self.get_session() as session:
            result = await session.execute(
                update(B)
                .where(B.id == entitlements_bucket_id)
                .values(
                    connection_slot_balance=func.greatest(
                        B.connection_slot_limit - open_connections, 0
                    ),
                    updated_at=_now(),
                )
                .returning(B)
            )
            row = result.scalar_one_or_none()
            if row is None:
                return None
            await session.commit()
            return EntitlementsBucket.model_validate(row)

    async def draw_traffic(
        self, entitlements_bucket_id: str, amount: int
    ) -> EntitlementsBucket | None:
        """Charge transferred bytes: included balance first, then grace.

        Single UPDATE, so the draw is atomic under concurrency. Traffic has
        already happened, so anything beyond balance+grace is dropped rather
        than leaving the balance negative.
        """
        grace_draw = func.least(
            B.grace_balance, func.greatest(amount - B.network_traffic_balance, 0)
        )
        async with self.get_session() as session:
            result = await session.execute(
                update(B)
                .where(B.id == entitlements_bucket_id)
                .values(
                    network_traffic_balance=func.greatest(
                        B.network_traffic_balance - amount, 0
                    ),
                    grace_balance=B.grace_balance - grace_draw,
                    updated_at=_now(),
                )
                .returning(B)
            )
            row = result.scalar_one_or_none()
            if row is None:
                return None
            await session.commit()
            return EntitlementsBucket.model_validate(row)

    async def grant_grace(
        self, entitlements_bucket_id: str
    ) -> EntitlementsBucket | None:
        """Grant the grace allowance at most once per exhaustion event.

        The WHERE clause is the guard: only fires while the included balance
        is empty and no grace has been granted since the last replenishment,
        so concurrent callers can't both win. Returns None if not granted.
        """
        async with self.get_session() as session:
            result = await session.execute(
                update(B)
                .where(B.id == entitlements_bucket_id)
                .where(B.network_traffic_balance == 0)
                .where(B.grace_granted_at.is_(None))
                .where(B.grace_policy_bytes > 0)
                .values(
                    grace_balance=B.grace_policy_bytes,
                    grace_granted_at=_now(),
                    updated_at=_now(),
                )
                .returning(B)
            )
            row = result.scalar_one_or_none()
            if row is None:
                return None
            await session.commit()
            return EntitlementsBucket.model_validate(row)

    async def credit_top_up(
        self, entitlements_bucket_id: str, top_up_bytes: int
    ) -> EntitlementsBucket | None:
        """Add a paid top-up block. Re-arms the grace allowance: the customer
        has just paid, so a later exhaustion is a fresh event."""
        async with self.get_session() as session:
            result = await session.execute(
                update(B)
                .where(B.id == entitlements_bucket_id)
                .values(
                    network_traffic_allowance=B.network_traffic_allowance
                    + top_up_bytes,
                    network_traffic_balance=B.network_traffic_balance + top_up_bytes,
                    grace_granted_at=None,
                    updated_at=_now(),
                )
                .returning(B)
            )
            row = result.scalar_one_or_none()
            if row is None:
                return None
            await session.commit()
            return EntitlementsBucket.model_validate(row)

    async def apply_plan(
        self,
        entitlements_bucket_id: str,
        *,
        allowance: int,
        slot_limit: int,
        grace_policy_bytes: int,
        stripe_managed: bool,
        reset_usage: bool,
        period_start: datetime | None = None,
        period_end: datetime | None = None,
    ) -> EntitlementsBucket | None:
        """Move the bucket onto a plan's limits.

        reset_usage=True starts a fresh period (renewal / new subscription):
        full allowance, grace cleared. reset_usage=False (mid-period plan
        change) preserves what's already been used, so upgrading can't be
        used to reset usage and downgrading can't strand the balance.
        Connection slots always preserve in-use slots; they aren't periodic.
        """
        if reset_usage:
            traffic_balance = allowance
        else:
            traffic_balance = func.greatest(
                allowance - (B.network_traffic_allowance - B.network_traffic_balance),
                0,
            )
        values: dict = {
            "network_traffic_allowance": allowance,
            "network_traffic_balance": traffic_balance,
            "connection_slot_balance": func.greatest(
                slot_limit - (B.connection_slot_limit - B.connection_slot_balance), 0
            ),
            "connection_slot_limit": slot_limit,
            "grace_policy_bytes": grace_policy_bytes,
            "stripe_managed_period": stripe_managed,
            "updated_at": _now(),
        }
        if reset_usage:
            values.update(grace_balance=0, grace_granted_at=None)
            values["period_start"] = period_start
            values["period_end"] = period_end
        async with self.get_session() as session:
            result = await session.execute(
                update(B)
                .where(B.id == entitlements_bucket_id)
                .values(**values)
                .returning(B)
            )
            row = result.scalar_one_or_none()
            if row is None:
                return None
            await session.commit()
            return EntitlementsBucket.model_validate(row)

    async def delete_entitlements_bucket(self, entitlements_bucket_id: str) -> bool:
        async with self.get_session() as session:
            bucket = await session.get(SQLEntitlementsBucket, entitlements_bucket_id)
            if bucket is None:
                return False
            await session.delete(bucket)
            await session.commit()
            return True
