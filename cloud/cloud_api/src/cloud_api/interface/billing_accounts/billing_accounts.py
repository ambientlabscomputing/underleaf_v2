from fastapi import APIRouter, Depends, HTTPException, status

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    BillingAccount,
    CreateBillingAccountRequest,
    CreateCheckoutSessionRequest,
    CreateCheckoutSessionResponse,
    ListBillingAccountResponse,
    ListInvoiceResponse,
    PatchBillingAccountRequest,
    QueryBillingAccountRequest,
    QueryInvoiceRequest,
    TopUp,
    UsageSummary,
)
from cloud_api.service.billing_account_service import (
    BillingAccountAlreadyExistsError,
    BillingAccountNotFoundError,
    BillingAccountService,
)
from cloud_api.service.invoice_service import InvoiceService
from cloud_api.service.plan_service import PlanPriceNotFoundError
from cloud_api.service.stripe_checkout_service import (
    PlanPriceNotSellableError,
    StripeCheckoutService,
    StripeNotConfiguredError,
)
from cloud_api.service.top_up_service import (
    TopUpInProgressError,
    TopUpNotAvailableError,
    TopUpPaymentError,
    TopUpService,
)
from cloud_api.service.usage_enforcement_service import UsageEnforcementService
from cloud_api.service.usage_summary_service import (
    UsageSummaryNotAvailableError,
    UsageSummaryService,
)
from cloud_api.service_manager import (
    get_billing_account_service,
    get_invoice_service,
    get_stripe_checkout_service,
    get_top_up_service,
    get_usage_enforcement_service,
    get_usage_summary_service,
)

router = APIRouter(prefix="/billing-accounts", tags=["Billing Accounts"])


async def _owned_account(
    billing_account_id: str,
    claims: AccessTokenClaims,
    service: BillingAccountService,
) -> BillingAccount:
    """Load a billing account the caller owns. A foreign account is reported
    as not-found rather than forbidden so IDs can't be probed."""
    try:
        account = await service.get_billing_account(billing_account_id)
    except BillingAccountNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Billing account not found"
        )
    if account.principal_account_id != claims.azp:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Billing account not found"
        )
    return account


@router.post(
    "",
    response_model=BillingAccount,
    status_code=status.HTTP_201_CREATED,
    summary="Create a billing account",
)
async def create_billing_account(
    req: CreateBillingAccountRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(
        get_billing_account_service
    ),
) -> BillingAccount:
    try:
        return await billing_account_service.create_billing_account(req, claims.azp)
    except BillingAccountAlreadyExistsError:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="This account already has a billing account",
        )


@router.get(
    "",
    response_model=ListBillingAccountResponse,
    summary="List billing accounts for the authenticated principal account",
)
async def get_billing_accounts(
    name: str | None = None,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(
        get_billing_account_service
    ),
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
    billing_account_service: BillingAccountService = Depends(
        get_billing_account_service
    ),
) -> BillingAccount:
    return await _owned_account(billing_account_id, claims, billing_account_service)


@router.patch(
    "/{billing_account_id}",
    response_model=BillingAccount,
    summary="Update a billing account (name, auto top-up settings)",
)
async def patch_billing_account(
    billing_account_id: str,
    req: PatchBillingAccountRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(
        get_billing_account_service
    ),
    enforcement: UsageEnforcementService = Depends(get_usage_enforcement_service),
) -> BillingAccount:
    await _owned_account(billing_account_id, claims, billing_account_service)
    account = await billing_account_service.patch_billing_account(
        billing_account_id, req
    )
    if req.auto_top_up_enabled:
        await enforcement.on_auto_top_up_enabled(billing_account_id)
    return account


@router.get(
    "/{billing_account_id}/usage",
    response_model=UsageSummary,
    summary="Allowance, overage state and alerts for the current period",
)
async def get_usage(
    billing_account_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(
        get_billing_account_service
    ),
    usage_summary_service: UsageSummaryService = Depends(get_usage_summary_service),
) -> UsageSummary:
    await _owned_account(billing_account_id, claims, billing_account_service)
    try:
        return await usage_summary_service.get_summary(billing_account_id)
    except UsageSummaryNotAvailableError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="No usage information for this billing account",
        )


@router.post(
    "/{billing_account_id}/top-ups",
    response_model=TopUp,
    status_code=status.HTTP_201_CREATED,
    summary="Buy a prepaid data block now (charged to the card on file)",
)
async def buy_top_up(
    billing_account_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(
        get_billing_account_service
    ),
    top_up_service: TopUpService = Depends(get_top_up_service),
) -> TopUp:
    await _owned_account(billing_account_id, claims, billing_account_service)
    try:
        return await top_up_service.purchase_manual(billing_account_id)
    except TopUpNotAvailableError as exc:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail=str(exc))
    except TopUpInProgressError as exc:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail=str(exc))
    except TopUpPaymentError as exc:
        raise HTTPException(
            status_code=status.HTTP_402_PAYMENT_REQUIRED, detail=str(exc)
        )


@router.post(
    "/{billing_account_id}/checkout-session",
    response_model=CreateCheckoutSessionResponse,
    summary="Create a Stripe Checkout session to subscribe this billing account to a plan",
)
async def create_checkout_session(
    billing_account_id: str,
    req: CreateCheckoutSessionRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(
        get_billing_account_service
    ),
    stripe_checkout_service: StripeCheckoutService = Depends(
        get_stripe_checkout_service
    ),
) -> CreateCheckoutSessionResponse:
    await _owned_account(billing_account_id, claims, billing_account_service)
    try:
        return await stripe_checkout_service.create_checkout_session(
            billing_account_id, req.plan_price_id
        )
    except BillingAccountNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Billing account not found"
        )
    except PlanPriceNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Plan price not found"
        )
    except StripeNotConfiguredError:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="Billing is not configured on this deployment",
        )
    except PlanPriceNotSellableError:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="Plan price is not linked to a purchasable Stripe price",
        )


@router.get(
    "/{billing_account_id}/invoices",
    response_model=ListInvoiceResponse,
    summary="List invoices for a billing account (read-only, mirrors Stripe)",
)
async def get_billing_account_invoices(
    billing_account_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(
        get_billing_account_service
    ),
    invoice_service: InvoiceService = Depends(get_invoice_service),
) -> ListInvoiceResponse:
    await _owned_account(billing_account_id, claims, billing_account_service)
    return await invoice_service.get_invoices(
        QueryInvoiceRequest(billing_account_id=billing_account_id)
    )


@router.delete(
    "/{billing_account_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    summary="Delete a billing account",
)
async def delete_billing_account(
    billing_account_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    billing_account_service: BillingAccountService = Depends(
        get_billing_account_service
    ),
) -> None:
    await _owned_account(billing_account_id, claims, billing_account_service)
    await billing_account_service.delete_billing_account(billing_account_id)
