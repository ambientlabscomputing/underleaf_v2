from datetime import UTC, datetime
from types import SimpleNamespace

import jwt
import pytest

from cloud_api.lib.assertion_lib import AssertionLib, InvalidAssertionRequestError
from cloud_api.service.assertion_service import (
    AssertionService,
    ClusterAccessDeniedError,
)


class FakeClusterRepo:
    def __init__(self, clusters):
        self.clusters = clusters

    async def get_cluster(self, cluster_id):
        return self.clusters.get(cluster_id)


@pytest.fixture
def svc(tmp_path):
    repo = FakeClusterRepo(
        {
            "cluster_mine": SimpleNamespace(
                id="cluster_mine", principal_account_id="acct_me"
            ),
            "cluster_theirs": SimpleNamespace(
                id="cluster_theirs", principal_account_id="acct_other"
            ),
        }
    )
    return AssertionService(AssertionLib(str(tmp_path / "k")), repo)


@pytest.mark.asyncio
async def test_owner_gets_an_assertion_bound_to_the_cluster(svc):
    tok = await svc.issue_for_cluster("acct_me", "cluster_mine", ["read", "deploy"])
    c = jwt.decode(tok, options={"verify_signature": False})
    assert (c["sub"], c["aud"], c["scopes"]) == (
        "acct_me",
        "cluster_mine",
        ["deploy", "read"],
    )
    assert c["exp"] - c["iat"] == 60
    assert abs(c["iat"] - datetime.now(UTC).timestamp()) < 5


@pytest.mark.asyncio
@pytest.mark.parametrize("cluster_id", ["cluster_theirs", "cluster_missing"])
async def test_other_accounts_and_missing_clusters_are_refused_alike(svc, cluster_id):
    with pytest.raises(ClusterAccessDeniedError):
        await svc.issue_for_cluster("acct_me", cluster_id, ["read"])


@pytest.mark.asyncio
async def test_invalid_scopes_are_refused(svc):
    with pytest.raises(InvalidAssertionRequestError):
        await svc.issue_for_cluster("acct_me", "cluster_mine", ["root"])
