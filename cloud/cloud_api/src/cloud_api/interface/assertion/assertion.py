"""
Assertion key set (RFD 5, "Assertion keys are fetched and cached").

  GET /api/v2/cloud/assertion-keys   -- JWKS of the cloud's assertion public keys

Unauthenticated on purpose: it publishes public keys only. Each key carries its
`kid` and `not_before`/`not_after` so verifiers can keep a rotated-out key
until it expires. There is intentionally no endpoint here that mints an
assertion; attaching one to a tunnelled request is step 7b.
"""

from fastapi import APIRouter, Depends, Response
from pydantic import BaseModel

from cloud_api.lib.assertion_lib import AssertionLib
from cloud_api.service_manager import get_assertion_lib

router = APIRouter(prefix="/assertion-keys", tags=["Assertion keys"])


class AssertionJWK(BaseModel):
    kty: str
    crv: str
    kid: str
    alg: str
    use: str
    x: str
    not_before: int
    not_after: int


class AssertionKeySet(BaseModel):
    keys: list[AssertionJWK]


@router.get(
    "",
    response_model=AssertionKeySet,
    summary="Public keys that verify cluster assertions (JWKS)",
)
async def get_assertion_keys(
    response: Response,
    assertion_lib: AssertionLib = Depends(get_assertion_lib),
) -> AssertionKeySet:
    # Short shared-cache lifetime: well inside the rotation overlap window.
    response.headers["Cache-Control"] = "public, max-age=300"
    return AssertionKeySet(**assertion_lib.jwks())
