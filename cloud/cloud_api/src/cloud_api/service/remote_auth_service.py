"""
RemoteAuthService -- the decision behind nginx's auth_request for remote access
(RFD 5, "Remote access"): who is calling, may they reach this cluster, and which
assertion and standing connection go with the request.

Two kinds of credential reach a cluster remotely, and exactly these two:

* a `ufctl` device key (`ufdk_...`), judged by DeviceKeyService only;
* an account OAuth access JWT, which is what Cockpit holds.

The kind is chosen by looking at the credential itself, never by trusting a
header: a credential starting with `ufdk_` is a device key and is never tried
as a JWT, and everything else is only ever tried as a JWT. A JWT must be a
signed, unexpired *user* access token. Refresh tokens (no `azp`) are refused,
and so are the access tokens x_subject_id_token_middleware_ mints for a
cluster or node (their `sub` is not a user id), so the X-Subject-Id bypass
cannot become a way into this endpoint even if something upstream forwarded it.
"""

from dataclasses import dataclass

from cloud_api.lib.assertion_lib import VALID_SCOPES
from cloud_api.lib.auth_lib import AuthLib, InvalidTokenError
from cloud_api.models.base import IDPrefix
from cloud_api.repository.connection_repository import ConnectionRepository
from cloud_api.service.assertion_service import AssertionService
from cloud_api.service.device_key_service import (
    DeviceKeyService,
    looks_like_device_key,
)

# An owner may do everything the Orchestrator can scope: read, deploy, admin.
# These are the strings in shared/assertion/claims.go (ScopeRead/Deploy/Admin)
# and edge/orchestrator/service/token_service.go; a test keeps them in step.
OWNER_SCOPES: tuple[str, ...] = ("read", "deploy", "admin")
assert set(OWNER_SCOPES) == set(VALID_SCOPES)

MAX_CREDENTIAL_LENGTH = 4096


@dataclass(frozen=True)
class RemoteGrant:
    assertion: str
    # Standing remote connection of the cluster, or "" when there is none.
    connection_id: str


class RemoteAuthService:
    def __init__(
        self,
        auth_lib: AuthLib,
        device_key_service: DeviceKeyService,
        assertion_service: AssertionService,
        conn_repo: ConnectionRepository,
    ) -> None:
        self.auth_lib = auth_lib
        self.device_key_service = device_key_service
        self.assertion_service = assertion_service
        self.conn_repo = conn_repo

    async def authenticate(self, credential: str) -> str | None:
        """The account id the credential acts for, or None. Never raises for a
        bad credential, never logs it."""
        if not credential or len(credential) > MAX_CREDENTIAL_LENGTH:
            return None
        if looks_like_device_key(credential):
            key = await self.device_key_service.authenticate(credential)
            return key.principal_account_id if key is not None else None
        try:
            claims = self.auth_lib.validate_token(credential)
        except InvalidTokenError:
            return None
        if not claims.azp:  # a refresh token
            return None
        if not claims.sub.startswith(f"{IDPrefix.USER.value}_"):
            return None  # a token minted for a cluster/node (X-Subject-Id)
        return claims.azp

    async def authorize(self, account_id: str, cluster_id: str) -> RemoteGrant:
        """Mint the per-request assertion and look up the standing connection.

        Raises ClusterAccessDeniedError, identically for a cluster that does
        not exist and one that belongs to someone else."""
        assertion = await self.assertion_service.issue_for_cluster(
            account_id, cluster_id, list(OWNER_SCOPES)
        )
        conn = await self.conn_repo.get_standing_remote_connection(cluster_id)
        return RemoteGrant(assertion=assertion, connection_id=conn.id if conn else "")
