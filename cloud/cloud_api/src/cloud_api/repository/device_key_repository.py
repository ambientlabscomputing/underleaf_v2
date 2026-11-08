from datetime import UTC, datetime

from sqlalchemy import or_, select, update

from cloud_api.models.sql import SQLDeviceKey
from cloud_api.repository.base_repository import BaseRepository


class DeviceKeyRepository(BaseRepository):
    async def create(
        self, id: str, principal_account_id: str, name: str, key_hash: str
    ) -> SQLDeviceKey:
        async with self.get_session() as session:
            now = datetime.now(UTC)
            row = SQLDeviceKey(
                id=id,
                principal_account_id=principal_account_id,
                name=name,
                key_hash=key_hash,
                created_at=now,
                updated_at=now,
            )
            session.add(row)
            await session.commit()
            await session.refresh(row)
            return row

    async def get_by_hash(self, key_hash: str) -> SQLDeviceKey | None:
        async with self.get_session() as session:
            return (
                await session.scalars(
                    select(SQLDeviceKey).where(SQLDeviceKey.key_hash == key_hash)
                )
            ).first()

    async def list_active_for_account(
        self, principal_account_id: str
    ) -> list[SQLDeviceKey]:
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLDeviceKey)
                .where(
                    SQLDeviceKey.principal_account_id == principal_account_id,
                    SQLDeviceKey.revoked_at.is_(None),
                )
                .order_by(SQLDeviceKey.created_at.desc())
            )
            return list(result.all())

    async def revoke(self, key_id: str, principal_account_id: str) -> bool:
        """Revoke a key of this account. False when no such key belongs to the
        account (missing and foreign are indistinguishable). Revoking an
        already revoked key succeeds and keeps the first revocation time."""
        async with self.get_session() as session:
            now = datetime.now(UTC)
            result = await session.execute(
                update(SQLDeviceKey)
                .where(
                    SQLDeviceKey.id == key_id,
                    SQLDeviceKey.principal_account_id == principal_account_id,
                    SQLDeviceKey.revoked_at.is_(None),
                )
                .values(revoked_at=now, updated_at=now)
            )
            if result.rowcount == 1:
                await session.commit()
                return True
            owned = await session.scalar(
                select(SQLDeviceKey.id).where(
                    SQLDeviceKey.id == key_id,
                    SQLDeviceKey.principal_account_id == principal_account_id,
                )
            )
            return owned is not None

    async def touch_last_used(
        self, key_id: str, now: datetime, older_than: datetime
    ) -> None:
        """Set last_used_at unless it was already set after `older_than`."""
        async with self.get_session() as session:
            await session.execute(
                update(SQLDeviceKey)
                .where(
                    SQLDeviceKey.id == key_id,
                    or_(
                        SQLDeviceKey.last_used_at.is_(None),
                        SQLDeviceKey.last_used_at < older_than,
                    ),
                )
                .values(last_used_at=now)
            )
            await session.commit()
