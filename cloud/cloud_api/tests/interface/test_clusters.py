"""/clusters/{id}: every route is scoped to the caller's own cluster (found
while running the remote-access stack for real: the id-addressed routes took
any signed-in account's token for any cluster, so a stranger could rename or
delete a cluster or rewrite its node set)."""

import pytest
from cryptography.hazmat.primitives.asymmetric import rsa
from fastapi.testclient import TestClient

import cloud_api
from cloud_api.interface.router import app
from cloud_api.lib.auth_lib import AuthLib
from cloud_api.models.api import Cluster, Node
from cloud_api.service.cluster_service import ClusterService
from cloud_api.service_manager import get_auth_lib, get_cluster_service

BASE = f"{cloud_api.app_config.api.base_path}/clusters"
ALICE, BOB = "principal_alice", "principal_bob"


class FakeClusterRepo:
    def __init__(self):
        self.clusters = {
            "cluster_alice": Cluster(
                id="cluster_alice", name="a", principal_account_id=ALICE
            )
        }

    async def get_cluster(self, cluster_id):
        return self.clusters.get(cluster_id)

    async def patch_cluster(self, cluster_id, req):
        c = self.clusters.get(cluster_id)
        if c is None:
            return None
        if req.name is not None:
            c = c.model_copy(update={"name": req.name})
            self.clusters[cluster_id] = c
        return c

    async def delete_cluster(self, cluster_id):
        return self.clusters.pop(cluster_id, None) is not None


class FakeNodeRepo:
    def __init__(self):
        self.nodes: dict[str, Node] = {}

    async def query_nodes(self, req):
        return [n for n in self.nodes.values() if n.cluster_id == req.cluster_id]

    async def create_node(self, req):
        n = Node(id=req.id, name=req.name, cluster_id=req.cluster_id)
        self.nodes[n.id] = n
        return n

    async def delete_node(self, node_id):
        self.nodes.pop(node_id, None)


@pytest.fixture
def env():
    auth_lib = AuthLib.__new__(AuthLib)
    auth_lib._private_key = rsa.generate_private_key(
        public_exponent=65537, key_size=2048
    )
    auth_lib._public_key = auth_lib._private_key.public_key()
    clusters, nodes = FakeClusterRepo(), FakeNodeRepo()
    app.dependency_overrides[get_auth_lib] = lambda: auth_lib
    app.dependency_overrides[get_cluster_service] = lambda: ClusterService(
        clusters, nodes
    )
    try:
        yield type(
            "Env",
            (),
            {
                "client": TestClient(app),
                "auth_lib": auth_lib,
                "clusters": clusters,
                "nodes": nodes,
            },
        )
    finally:
        app.dependency_overrides.pop(get_auth_lib, None)
        app.dependency_overrides.pop(get_cluster_service, None)


def _as(env, account):
    token = env.auth_lib.mint_access_token(
        user_id=f"user_{account}", principal_account_id=account
    )
    return {"Authorization": f"Bearer {token}"}


SYNC = {"nodes": [{"id": "node_1", "name": "n1", "cluster_id": "cluster_alice"}]}


def test_the_owner_can_use_every_route(env):
    h = _as(env, ALICE)
    assert env.client.get(f"{BASE}/cluster_alice", headers=h).status_code == 200
    r = env.client.patch(f"{BASE}/cluster_alice", headers=h, json={"name": "renamed"})
    assert r.status_code == 200 and r.json()["name"] == "renamed"
    assert (
        env.client.post(
            f"{BASE}/cluster_alice/nodes/sync", headers=h, json=SYNC
        ).status_code
        == 200
    )
    assert "node_1" in env.nodes.nodes
    assert env.client.delete(f"{BASE}/cluster_alice", headers=h).status_code == 204


def test_a_stranger_gets_the_same_404_as_for_a_missing_cluster(env):
    h = _as(env, BOB)
    calls = [
        lambda cid: env.client.get(f"{BASE}/{cid}", headers=h),
        lambda cid: env.client.patch(f"{BASE}/{cid}", headers=h, json={"name": "x"}),
        lambda cid: env.client.post(f"{BASE}/{cid}/nodes/sync", headers=h, json=SYNC),
        lambda cid: env.client.delete(f"{BASE}/{cid}", headers=h),
    ]
    for call in calls:
        foreign, missing = call("cluster_alice"), call("cluster_nope")
        assert foreign.status_code == missing.status_code == 404
        assert foreign.json() == missing.json()
    # and nothing happened to Alice's cluster
    assert env.clusters.clusters["cluster_alice"].name == "a"
    assert env.nodes.nodes == {}


def test_a_request_without_a_token_is_refused(env):
    assert env.client.get(f"{BASE}/cluster_alice").status_code in (401, 403)
