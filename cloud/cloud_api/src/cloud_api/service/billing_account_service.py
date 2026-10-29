from cloud_api import logger
from cloud_api.models.api import (
    BillingAccount,
    CreateBillingAccountRequest,
    InternalPatchBillingAccountRequest,
    ListBillingAccountResponse,
    PatchBillingAccountRequest,
    QueryBillingAccountRequest,
)
from cloud_api.repository.billing_account_repository import BillingAccountRepository


class BillingAccountNotFoundError(Exception):
    def __init__(self, billing_account_id: str) -> None:
        super().__init__(f"BillingAccount '{billing_account_id}' not found")
        self.billing_account_id = billing_account_id


class BillingAccountAlreadyExistsError(Exception):
    """A principal account has exactly one billing account (signup creates
    it). A second would be unreachable by node->billing resolution and would
    carry no entitlements bucket."""

    def __init__(self, principal_account_id: str) -> None:
        super().__init__(
            f"Principal account '{principal_account_id}' already has a billing account"
        )
        self.principal_account_id = principal_account_id


class BillingAccountService:
    def __init__(self, billing_account_repo: BillingAccountRepository) -> None:
        self.billing_account_repo = billing_account_repo

    async def create_billing_account(
        self, req: CreateBillingAccountRequest, principal_account_id: str
    ) -> BillingAccount:
        existing = await self.billing_account_repo.query_billing_accounts(
            QueryBillingAccountRequest(principal_account_id=principal_account_id)
        )
        if existing:
            raise BillingAccountAlreadyExistsError(principal_account_id)
        full_req = req.model_copy(update={"principal_account_id": principal_account_id})
        account = await self.billing_account_repo.create_billing_account(full_req)
        logger.bind(
            billing_account_id=account.id, principal_account_id=principal_account_id
        ).info("BillingAccount created")
        return account

    async def get_billing_accounts(
        self, req: QueryBillingAccountRequest
    ) -> ListBillingAccountResponse:
        items = await self.billing_account_repo.query_billing_accounts(req)
        return ListBillingAccountResponse(items=items, total=len(items))

    async def get_billing_account(self, billing_account_id: str) -> BillingAccount:
        account = await self.billing_account_repo.get_billing_account(
            billing_account_id
        )
        if account is None:
            raise BillingAccountNotFoundError(billing_account_id)
        return account

    async def patch_billing_account(
        self, billing_account_id: str, req: PatchBillingAccountRequest
    ) -> BillingAccount:
        # The repo takes the internal superset; a customer-facing request
        # can only ever carry the safe subset of fields.
        account = await self.billing_account_repo.patch_billing_account(
            billing_account_id, InternalPatchBillingAccountRequest(**req.model_dump())
        )
        if account is None:
            raise BillingAccountNotFoundError(billing_account_id)
        return account

    async def delete_billing_account(self, billing_account_id: str) -> None:
        deleted = await self.billing_account_repo.delete_billing_account(
            billing_account_id
        )
        if not deleted:
            raise BillingAccountNotFoundError(billing_account_id)
        logger.bind(billing_account_id=billing_account_id).info(
            "BillingAccount deleted"
        )
