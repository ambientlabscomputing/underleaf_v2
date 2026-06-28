from cloud_api.lib.auth_lib import AuthLib, InvalidCredentialsError, InvalidTokenError
from cloud_api.repository import user_repository
from cloud_api.models.api import SignInRequest, TokenResponse, User
from cloud_api import logger


class AuthService:
    def __init__(
        self,
        auth_lib: AuthLib,
        user_repo: user_repository.UserRepository,
    ):
        self.auth_lib = auth_lib
        self.user_repo = user_repo

    async def login(self, req: SignInRequest) -> TokenResponse:
        logger.bind(email=req.email).info("User login attempt")
        sign_in_response = await self.auth_lib.sign_in(req)
        user = sign_in_response.user
        access_token = self.auth_lib.mint_access_token(
            user.id, user.principal_account_id
        )
        refresh_token = self.auth_lib.mint_refresh_token(user.id)
        logger.bind(user_id=user.id).info("User login successful")
        return TokenResponse(access_token=access_token, refresh_token=refresh_token)

    async def whoami(self, token: str) -> User:
        claims = self.auth_lib.validate_token(token)
        user = await self.user_repo.get_user(claims.sub)
        if user is None:
            raise InvalidTokenError("User not found for token subject")
        return user

    async def refresh_token(self, refresh_token: str) -> TokenResponse:
        claims = self.auth_lib.validate_token(refresh_token)
        user = await self.user_repo.get_user(claims.sub)
        if user is None:
            raise InvalidTokenError("User not found for token subject")
        access_token = self.auth_lib.mint_access_token(
            user.id, user.principal_account_id
        )
        new_refresh_token = self.auth_lib.mint_refresh_token(user.id)
        logger.bind(user_id=user.id).info("Token refreshed")
        return TokenResponse(access_token=access_token, refresh_token=new_refresh_token)
