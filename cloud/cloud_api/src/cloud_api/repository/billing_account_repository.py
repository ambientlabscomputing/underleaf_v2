from cloud_api.models.api import (
    BillingAccount,
    CreateBillingAccountRequest,
    QueryBillingAccountRequest,
    PatchBillingAccountRequest,
)
from cloud_api.models.sql import SQLBillingAccount
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update
from datetime import datetime, timezone


class BillingAccountRepository(BaseRepository):
    async def create_billing_account(
        self, req: CreateBillingAccountRequest
    ) -> BillingAccount:
        async with self.get_session() as session:
            new_account = SQLBillingAccount(
                name=req.name,
                principal_account_id=req.principal_account_id,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
            )
            session.add(new_account)
            await session.commit()
            await session.refresh(new_account)
            return BillingAccount.model_validate(new_account)

    async def get_billing_account(
        self, billing_account_id: str
    ) -> BillingAccount | None:
        async with self.get_session() as session:
            account = await session.get(SQLBillingAccount, billing_account_id)
            if account is None:
                return None
            return BillingAccount.model_validate(account)

    async def query_billing_accounts(
        self, req: QueryBillingAccountRequest
    ) -> list[BillingAccount]:
        async with self.get_session() as session:
            query = select(SQLBillingAccount)
            if req.name is not None:
                query = query.filter(SQLBillingAccount.name == req.name)
            if req.principal_account_id is not None:
                query = query.filter(
                    SQLBillingAccount.principal_account_id == req.principal_account_id
                )
            result = await session.scalars(query)
            accounts = result.all()
            return [BillingAccount.model_validate(account) for account in accounts]

    async def patch_billing_account(
        self, billing_account_id: str, req: PatchBillingAccountRequest
    ) -> BillingAccount | None:
        async with self.get_session() as session:
            update_stmt = update(SQLBillingAccount).where(
                SQLBillingAccount.id == billing_account_id
            ).values(updated_at=datetime.now(timezone.utc))
            if req.name is not None:
                update_stmt = update_stmt.values(name=req.name)
            if req.stripe_customer_id is not None:
                update_stmt = update_stmt.values(
                    stripe_customer_id=req.stripe_customer_id
                )
            if req.stripe_data is not None:
                update_stmt = update_stmt.values(stripe_data=req.stripe_data)
            result = await session.execute(update_stmt)
            if result.rowcount == 0:
                return None
            await session.commit()
            updated_account = await session.get(SQLBillingAccount, billing_account_id)
            return BillingAccount.model_validate(updated_account)

    async def delete_billing_account(self, billing_account_id: str) -> bool:
        async with self.get_session() as session:
            account = await session.get(SQLBillingAccount, billing_account_id)
            if account is None:
                return False
            await session.delete(account)
            await session.commit()
            return True
