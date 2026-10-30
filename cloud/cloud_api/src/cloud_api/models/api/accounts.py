from pydantic import BaseModel, Field

from cloud_api.models.api.billing_accounts import BillingAccount
from cloud_api.models.api.subscriptions import Subscription
from cloud_api.models.base import Base, IDPrefix, generate_id


class PrincipalAccount(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.PRINCIPAL_ACCOUNT),
        description="Unique identifier for the principal account",
    )
    name: str = Field(..., description="Name of the principal account")


class CreatePrincipalAccountRequest(BaseModel):
    name: str = Field(..., description="Name of the principal account")


class PatchPrincipalAccountRequest(BaseModel):
    name: str | None = Field(None, description="Name of the principal account")


class QueryPrincipalAccountRequest(BaseModel):
    name: str | None = Field(None, description="Name of the principal account")


class User(Base):
    id: str = Field(
        default_factory=lambda: generate_id(IDPrefix.USER),
        description="Unique identifier for the user",
    )
    name: str = Field(..., description="Name of the user")
    email: str = Field(..., description="Email address of the user")
    principal_account_id: str = Field(
        ..., description="ID of the associated principal account"
    )


class CreateUserRequest(BaseModel):
    name: str = Field(..., description="Name of the user")
    password: str = Field(..., description="Password for the user")
    email: str = Field(..., description="Email address of the user")


class PatchUserRequest(BaseModel):
    name: str | None = Field(None, description="Name of the user")
    email: str | None = Field(None, description="Email address of the user")


class QueryUserRequest(BaseModel):
    name: str | None = Field(None, description="Name of the user")
    email: str | None = Field(None, description="Email address of the user")


class UserPassword(Base):
    user_id: str = Field(..., description="ID of the associated user")
    password_hash: str = Field(..., description="Hashed password for the user")


class InternalUser(User):
    user_password: UserPassword = Field(
        ..., description="Password information for the user"
    )


class UserSignUpResponse(BaseModel):
    user: User = Field(..., description="The created user")
    principal_account: PrincipalAccount = Field(
        ..., description="The created principal account"
    )
    billing_account: BillingAccount = Field(
        ..., description="The created billing account"
    )
    subscription: Subscription = Field(..., description="The created subscription")
