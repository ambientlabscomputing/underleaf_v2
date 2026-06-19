from cloud_api.repository import (
    principal_account_repository,
    billing_account_repository,
    usage_event_repository,
    user_repository,
    subscription_repository,
)
from cloud_api.models.api import (
    SubscriptionTier,
    UserSignUpResponse,
)
from cloud_api import logger
from argon2 import PasswordHasher

_ph = PasswordHasher()


class AccountService:
    def __init__(
        self,
        principal_account_repo: principal_account_repository.PrincipalAccountRepository,
        billing_account_repo: billing_account_repository.BillingAccountRepository,
        usage_event_repo: usage_event_repository.UsageEventRepository,
        user_repo: user_repository.UserRepository,
        subscription_repo: subscription_repository.SubscriptionRepository,
    ):
        self.principal_account_repo = principal_account_repo
        self.billing_account_repo = billing_account_repo
        self.usage_event_repo = usage_event_repo
        self.subscription_repo = subscription_repo
        self.user_repo = user_repo

    async def sign_up(
        self, user_request: user_repository.CreateUserRequest
    ) -> UserSignUpResponse:
        logger.bind(
            user_request=user_request.model_dump(mode="json", exclude={"password"})
        ).info("Fullfilling user sign-up process")
        # Hash the password before persisting anything
        password_hash = _ph.hash(user_request.password)

        # Create a new principal account
        principal_account = await self._create_principal_account(
            name=f"{user_request.name}'s Principal Account"
        )

        # Create a new billing account associated with the principal account
        billing_account = await self._create_billing_account(
            name=f"{user_request.name}'s Billing Account",
            principal_account_id=principal_account.id,
        )
        subscription = await self._create_subscription(billing_account.id)
        logger.bind(billing_account=billing_account.model_dump(mode="json")).info(
            "Created billing account"
        )
        # Create a new user associated with the principal account
        user = await self.user_repo.create_user(user_request, principal_account.id)

        # Persist the hashed password (salt is embedded in the hash string)
        await self.user_repo.create_user_password(user.id, password_hash)

        response = UserSignUpResponse(
            user=user,
            principal_account=principal_account,
            billing_account=billing_account,
            subscription=subscription,
        )
        logger.bind(response=response.model_dump(mode="json")).info(
            "User sign-up process completed"
        )
        return response

    async def _create_principal_account(
        self, name: str
    ) -> principal_account_repository.PrincipalAccount:
        return await self.principal_account_repo.create_principal_account(
            principal_account_repository.CreatePrincipalAccountRequest(name=name)
        )

    async def _create_billing_account(
        self, name: str, principal_account_id: str
    ) -> billing_account_repository.BillingAccount:
        return await self.billing_account_repo.create_billing_account(
            billing_account_repository.CreateBillingAccountRequest(
                name=name, principal_account_id=principal_account_id
            )
        )

    async def _create_subscription(
        self, billing_account_id: str
    ) -> subscription_repository.Subscription:
        return await self.subscription_repo.create_subscription(
            subscription_repository.CreateSubscriptionRequest(
                billing_account_id=billing_account_id,
                tier=SubscriptionTier.FREE,
            )
        )
