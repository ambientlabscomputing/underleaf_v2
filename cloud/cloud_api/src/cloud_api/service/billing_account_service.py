from cloud_api.models.api import (
    BillingAccount,
    CreateBillingAccountRequest,
    ListBillingAccountResponse,
    PatchBillingAccountRequest,
    QueryBillingAccountRequest,
)
from cloud_api.repository.billing_account_repository import BillingAccountRepository
from cloud_api import logger


class BillingAccountNotFoundError(Exception):
    def __init__(self, billing_account_id: str) -> None:
        super().__init__(f"BillingAccount '{billing_account_id}' not found")
        self.billing_account_id = billing_account_id


class BillingAccountService:
    def __init__(self, billing_account_repo: BillingAccountRepository) -> None:
        self.billing_account_repo = billing_account_repo

    async def create_billing_account(
        self, req: CreateBillingAccountRequest, principal_account_id: str
    ) -> BillingAccount:
        full_req = req.model_copy(update={"principal_account_id": principal_account_id})
        account = await self.billing_account_repo.create_billing_account(full_req)
        logger.bind(billing_account_id=account.id, principal_account_id=principal_account_id).info(
            "BillingAccount created"
        )
        return account

    async def get_billing_accounts(
        self, req: QueryBillingAccountRequest
    ) -> ListBillingAccountResponse:
        items = await self.billing_account_repo.query_billing_accounts(req)
        return ListBillingAccountResponse(items=items, total=len(items))

    async def get_billing_account(self, billing_account_id: str) -> BillingAccount:
        account = await self.billing_account_repo.get_billing_account(billing_account_id)
        if account is None:
            raise BillingAccountNotFoundError(billing_account_id)
        return account

    async def patch_billing_account(
        self, billing_account_id: str, req: PatchBillingAccountRequest
    ) -> BillingAccount:
        account = await self.billing_account_repo.patch_billing_account(billing_account_id, req)
        if account is None:
            raise BillingAccountNotFoundError(billing_account_id)
        return account

    async def delete_billing_account(self, billing_account_id: str) -> None:
        deleted = await self.billing_account_repo.delete_billing_account(billing_account_id)
        if not deleted:
            raise BillingAccountNotFoundError(billing_account_id)
        logger.bind(billing_account_id=billing_account_id).info("BillingAccount deleted")
