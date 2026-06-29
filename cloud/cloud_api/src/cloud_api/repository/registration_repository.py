from cloud_api.models.api import ClusterCandidate
from cloud_api.models.sql import SQLClusterCandidate
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update
from datetime import datetime, timezone


class RegistrationRepository(BaseRepository):
    async def create_candidate(
        self,
        id: str,
        user_code: str,
        device_code_hash: str,
        proposed_cluster_name: str,
        proposed_cluster_id: str,
        device_code_expires_at: datetime,
        poll_interval: int,
    ) -> SQLClusterCandidate:
        async with self.get_session() as session:
            candidate = SQLClusterCandidate(
                id=id,
                user_code=user_code,
                device_code_hash=device_code_hash,
                status="pending",
                proposed_cluster_name=proposed_cluster_name,
                proposed_cluster_id=proposed_cluster_id,
                device_code_expires_at=device_code_expires_at,
                poll_interval=poll_interval,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
            )
            session.add(candidate)
            await session.commit()
            await session.refresh(candidate)
            return candidate

    async def get_by_user_code(self, user_code: str) -> SQLClusterCandidate | None:
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLClusterCandidate).where(
                    SQLClusterCandidate.user_code == user_code
                )
            )
            return result.first()

    async def get_by_device_code_hash(
        self, device_code_hash: str
    ) -> SQLClusterCandidate | None:
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLClusterCandidate).where(
                    SQLClusterCandidate.device_code_hash == device_code_hash
                )
            )
            return result.first()

    async def approve(
        self,
        candidate_id: str,
        principal_account_id: str,
        cluster_id: str,
        one_time_token_hash: str,
        one_time_token_expires_at: datetime,
    ) -> None:
        async with self.get_session() as session:
            await session.execute(
                update(SQLClusterCandidate)
                .where(SQLClusterCandidate.id == candidate_id)
                .values(
                    status="approved",
                    principal_account_id=principal_account_id,
                    cluster_id=cluster_id,
                    one_time_token_hash=one_time_token_hash,
                    one_time_token_expires_at=one_time_token_expires_at,
                    updated_at=datetime.now(timezone.utc),
                )
            )
            await session.commit()

    async def consume(self, candidate_id: str) -> None:
        """Mark as consumed after the cert has been issued."""
        async with self.get_session() as session:
            await session.execute(
                update(SQLClusterCandidate)
                .where(SQLClusterCandidate.id == candidate_id)
                .values(status="consumed", updated_at=datetime.now(timezone.utc))
            )
            await session.commit()

    async def touch_poll(self, candidate_id: str) -> None:
        async with self.get_session() as session:
            await session.execute(
                update(SQLClusterCandidate)
                .where(SQLClusterCandidate.id == candidate_id)
                .values(
                    last_polled_at=datetime.now(timezone.utc),
                    updated_at=datetime.now(timezone.utc),
                )
            )
            await session.commit()

    async def find_candidate_by_token_hash(self, token_hash: str, status: str = "approved") -> SQLClusterCandidate | None:
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLClusterCandidate).where(
                    SQLClusterCandidate.one_time_token_hash == token_hash,
                    SQLClusterCandidate.status == status
                )
            )
            return result.first()
