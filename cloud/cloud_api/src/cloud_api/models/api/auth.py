from pydantic import BaseModel, Field

from cloud_api.models.api.accounts import User


class SignInRequest(BaseModel):
    email: str = Field(..., description="Email address of the user")
    password: str = Field(..., description="Password for the user")


class SignInResponse(BaseModel):
    user: User = Field(..., description="The authenticated user")


class TokenResponse(BaseModel):
    access_token: str = Field(..., description="Short-lived JWT access token")
    refresh_token: str = Field(..., description="Long-lived JWT refresh token")
    token_type: str = Field(default="bearer", description="Token type")
