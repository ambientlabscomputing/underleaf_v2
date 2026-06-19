from cloud_api.models.api import (
    PrincipalAccount,
    CreatePrincipalAccountRequest,
    QueryPrincipalAccountRequest,
    PatchPrincipalAccountRequest,
)
from cloud_api.models.sql import SQLPrincipalAccount
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update
from datetime import datetime, timezone


class PrincipalAccountRepository(BaseRepository):
    async def create_principal_account(
        self, req: CreatePrincipalAccountRequest
    ) -> PrincipalAccount:
        async with self.get_session() as session:
            new_account = SQLPrincipalAccount(
                name=req.name,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
            )
            session.add(new_account)
            await session.commit()
            await session.refresh(new_account)
            return PrincipalAccount.model_validate(new_account)

    async def get_principal_account(
        self, principal_account_id: str
    ) -> PrincipalAccount | None:
        async with self.get_session() as session:
            account = await session.get(SQLPrincipalAccount, principal_account_id)
            if account is None:
                return None
            return PrincipalAccount.model_validate(account)

    async def query_principal_accounts(
        self, req: QueryPrincipalAccountRequest
    ) -> list[PrincipalAccount]:
        async with self.get_session() as session:
            query = select(SQLPrincipalAccount)
            if req.name is not None:
                query = query.filter(SQLPrincipalAccount.name == req.name)
            result = await session.scalars(query)
            accounts = result.all()
            return [PrincipalAccount.model_validate(account) for account in accounts]

    async def patch_principal_account(
        self, principal_account_id: str, req: PatchPrincipalAccountRequest
    ) -> PrincipalAccount | None:
        async with self.get_session() as session:
            update_stmt = update(SQLPrincipalAccount).where(
                SQLPrincipalAccount.id == principal_account_id
            ).values(updated_at=datetime.now(timezone.utc))
            if req.name is not None:
                update_stmt = update_stmt.values(name=req.name)
            result = await session.execute(update_stmt)
            if result.scalar() is None:
                return None
            await session.commit()
            updated_account = await session.get(
                SQLPrincipalAccount, principal_account_id
            )
            return PrincipalAccount.model_validate(updated_account)

    async def delete_principal_account(self, principal_account_id: str) -> bool:
        async with self.get_session() as session:
            account = await session.get(SQLPrincipalAccount, principal_account_id)
            if account is None:
                return False
            await session.delete(account)
            await session.commit()
            return True
