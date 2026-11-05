# AssertionService
#   issue_for_cluster(account_id, cluster_id, scopes) returns (assertion token);
#
# The only place that decides *whether* an assertion may be minted. The
# transport that attaches it to a tunnelled request is RFD 5 step 7b.

from cloud_api import logger
from cloud_api.lib.assertion_lib import AssertionLib
from cloud_api.repository.cluster_repository import ClusterRepository


class ClusterAccessDeniedError(Exception):
    """The cluster does not exist or does not belong to the account. One error
    for both, so callers cannot probe for other accounts' cluster ids."""

    def __init__(self, cluster_id: str) -> None:
        super().__init__(f"Cluster '{cluster_id}' not found")
        self.cluster_id = cluster_id


class AssertionService:
    def __init__(
        self, assertion_lib: AssertionLib, cluster_repo: ClusterRepository
    ) -> None:
        self.assertion_lib = assertion_lib
        self.cluster_repo = cluster_repo

    async def issue_for_cluster(
        self, account_id: str, cluster_id: str, scopes: list[str]
    ) -> str:
        cluster = await self.cluster_repo.get_cluster(cluster_id)
        if cluster is None or cluster.principal_account_id != account_id:
            raise ClusterAccessDeniedError(cluster_id)
        logger.bind(cluster_id=cluster_id, account_id=account_id).info(
            "Issuing cluster assertion"
        )
        return self.assertion_lib.mint(
            account_id=account_id, cluster_id=cluster_id, scopes=scopes
        )
