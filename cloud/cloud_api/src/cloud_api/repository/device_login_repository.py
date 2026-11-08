from datetime import UTC, datetime

from sqlalchemy import select, update

from cloud_api.models.sql import SQLDeviceLogin
from cloud_api.repository.base_repository import BaseRepository


class DeviceLoginRepository(BaseRepository):
    async def create(
        self,
        id: str,
        user_code: str,
        device_code_hash: str,
        device_name: str,
        expires_at: datetime,
        poll_interval: int,
    ) -> SQLDeviceLogin:
        async with self.get_session() as session:
            now = datetime.now(UTC)
            row = SQLDeviceLogin(
                id=id,
                user_code=user_code,
                device_code_hash=device_code_hash,
                status="pending",
                device_name=device_name,
                expires_at=expires_at,
                poll_interval=poll_interval,
                created_at=now,
                updated_at=now,
            )
            session.add(row)
            await session.commit()
            await session.refresh(row)
            return row

    async def get_by_user_code(self, user_code: str) -> SQLDeviceLogin | None:
        async with self.get_session() as session:
            return (
                await session.scalars(
                    select(SQLDeviceLogin).where(SQLDeviceLogin.user_code == user_code)
                )
            ).first()

    async def get_by_device_code_hash(
        self, device_code_hash: str
    ) -> SQLDeviceLogin | None:
        async with self.get_session() as session:
            return (
                await session.scalars(
                    select(SQLDeviceLogin).where(
                        SQLDeviceLogin.device_code_hash == device_code_hash
                    )
                )
            ).first()

    async def decide(
        self,
        request_id: str,
        status: str,
        principal_account_id: str,
        now: datetime,
    ) -> bool:
        """pending -> approved/denied, once. False when the request was not
        pending (already decided, consumed) or had expired."""
        async with self.get_session() as session:
            result = await session.execute(
                update(SQLDeviceLogin)
                .where(
                    SQLDeviceLogin.id == request_id,
                    SQLDeviceLogin.status == "pending",
                    SQLDeviceLogin.expires_at > now,
                )
                .values(
                    status=status,
                    principal_account_id=principal_account_id,
                    updated_at=now,
                )
            )
            await session.commit()
            return result.rowcount == 1

    async def consume(self, request_id: str, now: datetime) -> bool:
        """approved -> consumed, once. Of any number of concurrent polls
        exactly one gets True and may mint the device key."""
        async with self.get_session() as session:
            result = await session.execute(
                update(SQLDeviceLogin)
                .where(
                    SQLDeviceLogin.id == request_id,
                    SQLDeviceLogin.status == "approved",
                    SQLDeviceLogin.expires_at > now,
                )
                .values(status="consumed", updated_at=now)
            )
            await session.commit()
            return result.rowcount == 1

    async def record_poll(
        self, request_id: str, now: datetime, poll_interval: int
    ) -> None:
        async with self.get_session() as session:
            await session.execute(
                update(SQLDeviceLogin)
                .where(SQLDeviceLogin.id == request_id)
                .values(last_polled_at=now, poll_interval=poll_interval, updated_at=now)
            )
            await session.commit()

    async def raise_poll_interval(
        self, request_id: str, poll_interval: int, now: datetime
    ) -> None:
        """A client polled too fast: make it wait longer (RFC 8628 slow_down)."""
        async with self.get_session() as session:
            await session.execute(
                update(SQLDeviceLogin)
                .where(SQLDeviceLogin.id == request_id)
                .values(poll_interval=poll_interval, updated_at=now)
            )
            await session.commit()
