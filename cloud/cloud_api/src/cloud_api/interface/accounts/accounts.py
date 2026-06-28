from fastapi import APIRouter, Depends, HTTPException, status

from cloud_api.models.api import CreateUserRequest, UserSignUpResponse
from cloud_api.service.account_service import AccountService
from cloud_api.service_manager import get_account_service

router = APIRouter(prefix="/accounts", tags=["Accounts"])


@router.post(
    "/signup",
    response_model=UserSignUpResponse,
    status_code=status.HTTP_201_CREATED,
    summary="Register a new user and provision their account",
)
async def sign_up(
    req: CreateUserRequest,
    account_service: AccountService = Depends(get_account_service),
) -> UserSignUpResponse:
    return await account_service.sign_up(req)
