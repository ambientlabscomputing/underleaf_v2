from fastapi import APIRouter, Depends, HTTPException, status

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    BillingAccount,
    CreateBillingAccountRequest,
    ListBillingAccountResponse,
    PatchBillingAccountRequest,
    QueryBillingAccountRequest,
)
from cloud_api.service.billing_account_service import (
    BillingAccountNotFoundError,
    BillingAccountService,
)
from cloud_api.service_manager import get_billing_account_service

router = APIRouter(prefix="/billing-accounts", tags=["Billing Accounts"])


@router.post(
    "",
    response_model=BillingAccount,
    status_code=status.HTTP_201_CREATED,
    summary="Create a billing account",
)
async def create_billing_account(
    req: CreateBillingAccountRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(get_billing_account_service),
) -> BillingAccount:
    return await billing_account_service.create_billing_account(req, claims.azp)


@router.get(
    "",
    response_model=ListBillingAccountResponse,
    summary="List billing accounts for the authenticated principal account",
)
async def get_billing_accounts(
    name: str | None = None,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(get_billing_account_service),
) -> ListBillingAccountResponse:
    return await billing_account_service.get_billing_accounts(
        QueryBillingAccountRequest(name=name, principal_account_id=claims.azp)
    )


@router.get(
    "/{billing_account_id}",
    response_model=BillingAccount,
    summary="Get a billing account by ID",
)
async def get_billing_account(
    billing_account_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(get_billing_account_service),
) -> BillingAccount:
    try:
        return await billing_account_service.get_billing_account(billing_account_id)
    except BillingAccountNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Billing account not found")


@router.patch(
    "/{billing_account_id}",
    response_model=BillingAccount,
    summary="Update a billing account",
)
async def patch_billing_account(
    billing_account_id: str,
    req: PatchBillingAccountRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(get_billing_account_service),
) -> BillingAccount:
    try:
        return await billing_account_service.patch_billing_account(billing_account_id, req)
    except BillingAccountNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Billing account not found")


@router.delete(
    "/{billing_account_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    summary="Delete a billing account",
)
async def delete_billing_account(
    billing_account_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(get_billing_account_service),
) -> None:
    try:
        await billing_account_service.delete_billing_account(billing_account_id)
    except BillingAccountNotFoundError:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Billing account not found")
