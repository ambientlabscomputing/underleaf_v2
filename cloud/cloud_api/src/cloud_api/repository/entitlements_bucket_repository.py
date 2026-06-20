from cloud_api.models.api import (
    EntitlementsBucket,
    CreateEntitlementsBucketRequest,
    QueryEntitlementsBucketRequest,
    PatchEntitlementsBucketRequest,
)
from cloud_api.models.sql import SQLEntitlementsBucket
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update
from datetime import datetime, timezone


class EntitlementsBucketRepository(BaseRepository):
    async def create_entitlements_bucket(
        self, req: CreateEntitlementsBucketRequest
    ) -> EntitlementsBucket:
        async with self.get_session() as session:
            new_bucket = SQLEntitlementsBucket(
                name=req.name,
                billing_account_id=req.billing_account_id,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
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
            buckets = result.all()
            return [EntitlementsBucket.model_validate(bucket) for bucket in buckets]

    async def patch_entitlements_bucket(
        self, entitlements_bucket_id: str, req: PatchEntitlementsBucketRequest
    ) -> EntitlementsBucket | None:
        async with self.get_session() as session:
            update_stmt = update(SQLEntitlementsBucket).where(
                SQLEntitlementsBucket.id == entitlements_bucket_id
            ).values(updated_at=datetime.now(timezone.utc))
            if req.name is not None:
                update_stmt = update_stmt.values(name=req.name)
            if req.network_traffic_balance is not None:
                update_stmt = update_stmt.values(
                    network_traffic_balance=req.network_traffic_balance
                )
            if req.connection_slot_balance is not None:
                update_stmt = update_stmt.values(
                    connection_slot_balance=req.connection_slot_balance
                )
            result = await session.execute(update_stmt)
            if result.rowcount == 0:
                return None
            await session.commit()
            updated_bucket = await session.get(
                SQLEntitlementsBucket, entitlements_bucket_id
            )
            return EntitlementsBucket.model_validate(updated_bucket)

    async def delete_entitlements_bucket(self, entitlements_bucket_id: str) -> bool:
        async with self.get_session() as session:
            bucket = await session.get(SQLEntitlementsBucket, entitlements_bucket_id)
            if bucket is None:
                return False
            await session.delete(bucket)
            await session.commit()
            return True
