"""The queued ("202 + async") connection flow against real Postgres: a
connection is recorded PENDING with its slot held, a durable task waits for a
worker, and the runner creates it on the worker under the same id.

The worker side is faked (FakeWorkers + the fake gRPC stub); everything else --
repositories, ConnectionService, TaskService, the facade client -- is real.
"""

import asyncio
from datetime import UTC, datetime, timedelta
from types import SimpleNamespace

import pytest
import pytest_asyncio
from sqlalchemy import text

from cloud_api.config import WorkerDefaults
from cloud_api.lib.conn_worker_client import conn_worker_pb2 as pb2
from cloud_api.lib.conn_worker_client.conn_worker_client import ConnWorkerClient
from cloud_api.lib.events import EventBus
from cloud_api.models.api import (
    Connection,
    ConnectionState,
    CreateConnectionRequest,
    NewStreamRequest,
    Status,
)
from cloud_api.models.api.worker import WorkerTaskKind, WorkerTaskState
from cloud_api.repository import session as db_session
from cloud_api.repository.worker_task_repository import WorkerTaskRepository
from cloud_api.service.connection_service import ConnectionService
from cloud_api.service.dynamic_conn_worker_client import DynamicConnWorkerClient
from cloud_api.service.stream_service import ConnectionNotReadyError
from cloud_api.service.task_service import TaskService
from cloud_api.service.worker_service import WorkerUnavailableError
from tests.integration.conftest import FakeConnWorkerStub

pytestmark = pytest.mark.asyncio


class FlakyStub(FakeConnWorkerStub):
    """Fails the first `fail_creates` CreateConnection calls; `before_create`
    lets a test act in the middle of a create."""

    def __init__(self):
        super().__init__()
        self.fail_creates = 0
        self.creates = 0
        self.before_create = None

    async def CreateConnection(self, req):
        self.creates += 1
        if self.fail_creates > 0:
            self.fail_creates -= 1
            raise RuntimeError("worker said no")
        if self.before_create is not None:
            await self.before_create(req)
        return await super().CreateConnection(req)


class FakeWorkers:
    """The slice of WorkerService the connection flow uses."""

    def __init__(self, client: ConnWorkerClient):
        self.client = client
        self.ready = False
        self.requested = 0
        self.unavailable: Exception | None = None

    async def get_ready_worker(self, worker_type):
        return SimpleNamespace(id="worker_fake") if self.ready else None

    async def request_worker(self, worker_type):
        if self.unavailable is not None:
            raise self.unavailable
        self.requested += 1

    async def get_client(self, worker):
        return self.client

    async def close(self):
        pass


@pytest_asyncio.fixture
async def env(db, tenant):
    from cloud_api import service_manager as sm

    # Other tests leave tasks behind; the runner must only see this test's.
    async with db_session.get_session() as s:
        await s.execute(
            text(
                "UPDATE underleaf.worker_tasks SET state='cancelled' "
                "WHERE state IN ('queued','running')"
            )
        )
        await s.commit()

    stub = FlakyStub()
    client = ConnWorkerClient(target="fake")
    client._channel = SimpleNamespace()
    client._stub = stub
    workers = FakeWorkers(client)
    bus = EventBus()
    settings = WorkerDefaults(
        task_poll_seconds=0,  # a released task is due again immediately
        task_retry_backoff_seconds=0,
        task_max_attempts=3,
        task_timeout_seconds=900,
    )
    task_repo = WorkerTaskRepository()
    tasks = TaskService(
        task_repo=task_repo,
        conn_repo=sm.get_conn_repo(),
        billing_account_repo=sm.get_billing_account_repo(),
        entitlements_bucket_service=sm.get_entitlements_bucket_service(),
        worker_service=workers,
        event_repo=sm.get_worker_event_repo(),
        event_bus=bus,
        settings=settings,
    )
    svc = ConnectionService(
        conn_repo=sm.get_conn_repo(),
        conn_client=DynamicConnWorkerClient(workers),
        billing_account_repo=sm.get_billing_account_repo(),
        entitlements_bucket_service=sm.get_entitlements_bucket_service(),
        task_service=tasks,
        event_bus=bus,
    )

    async def slots() -> int:
        bucket = await sm.get_entitlements_bucket_repo().get_entitlements_bucket_by_billing_account(
            tenant.billing_account_id
        )
        return bucket.connection_slot_balance

    return SimpleNamespace(
        svc=svc,
        tasks=tasks,
        task_repo=task_repo,
        workers=workers,
        stub=stub,
        bus=bus,
        tenant=tenant,
        slots=slots,
        settings=settings,
    )


async def _create(env, name="c1") -> Connection:
    return await env.svc.create_connection(
        CreateConnectionRequest(name=name, node_id=env.tenant.node_id)
    )


# -- queue repository -------------------------------------------------------


@pytest.fixture
def repo():
    return WorkerTaskRepository()


async def test_concurrent_claims_never_overlap(env, repo):
    for i in range(6):
        await repo.enqueue(WorkerTaskKind.CREATE_CONNECTION, connection_id=f"c{i}")

    a, b = await asyncio.gather(
        repo.claim(4, lease_seconds=60), repo.claim(4, lease_seconds=60)
    )

    ids_a, ids_b = {t.id for t in a}, {t.id for t in b}
    assert not ids_a & ids_b
    assert len(ids_a | ids_b) == 6


async def test_future_tasks_are_not_claimed(env, repo):
    await repo.enqueue(
        WorkerTaskKind.CREATE_CONNECTION,
        connection_id="later",
        run_after=datetime.now(UTC) + timedelta(hours=1),
    )

    assert await repo.claim(10, lease_seconds=60) == []


async def test_lapsed_lease_is_reclaimed_and_counts_an_attempt(env, repo):
    await repo.enqueue(WorkerTaskKind.CREATE_CONNECTION, connection_id="crashy")
    [first] = await repo.claim(1, lease_seconds=-1)  # runner "died" instantly
    assert first.attempts == 0

    [again] = await repo.claim(1, lease_seconds=60)

    assert again.id == first.id
    assert again.attempts == 1
    assert await repo.claim(1, lease_seconds=60) == []  # lease is live now


async def test_release_keeps_the_attempt_and_retry_spends_one(env, repo):
    task = await repo.enqueue(WorkerTaskKind.CREATE_CONNECTION, connection_id="r")
    await repo.claim(1, lease_seconds=60)
    await repo.release(task.id, datetime.now(UTC))
    assert (await repo.get(task.id)).attempts == 0

    await repo.claim(1, lease_seconds=60)
    await repo.retry(task.id, datetime.now(UTC), "boom")

    after = await repo.get(task.id)
    assert (after.state, after.attempts, after.error) == (
        WorkerTaskState.QUEUED,
        1,
        "boom",
    )


async def test_finishing_a_task_twice_only_counts_once(env, repo):
    task = await repo.enqueue(WorkerTaskKind.CREATE_CONNECTION, connection_id="f")
    await repo.claim(1, lease_seconds=60)

    assert await repo.succeed(task.id)
    assert not await repo.fail(task.id, "late")
    assert (await repo.get(task.id)).state == WorkerTaskState.SUCCEEDED


# -- the flow ----------------------------------------------------------------


async def test_create_returns_pending_holding_a_slot_and_queues_work(env):
    before = await env.slots()

    conn = await _create(env)

    assert (conn.state, conn.status) == (ConnectionState.PENDING, Status.IN_PROGRESS)
    assert await env.slots() == before - 1
    task = await env.task_repo.get_for_connection(conn.id)
    assert task.state == WorkerTaskState.QUEUED
    assert env.workers.requested == 1  # demand for a worker was recorded
    assert await env.tasks.has_pending_work()
    assert conn.id not in env.stub.connections  # nothing touched a worker yet


async def test_waiting_for_a_worker_spends_no_attempts(env):
    conn = await _create(env)

    for _ in range(5):
        await env.tasks.run_once()

    task = await env.task_repo.get_for_connection(conn.id)
    assert task.state == WorkerTaskState.QUEUED
    assert task.attempts == 0
    reread = await env.svc.conn_repo.get_connection(conn.id)
    assert reread.state == ConnectionState.PENDING
    assert reread.status_detail == "waiting for a worker to start"


async def test_ready_worker_creates_the_connection_under_the_same_id(env):
    conn = await _create(env)
    await env.tasks.run_once()
    env.workers.ready = True

    await env.tasks.run_once()

    assert conn.id in env.stub.connections  # the worker honoured our id
    done = await env.svc.conn_repo.get_connection(conn.id)
    assert (done.state, done.status) == (ConnectionState.PROVISIONED, Status.SUCCEEDED)
    assert done.status_detail is None
    task = await env.task_repo.get_for_connection(conn.id)
    assert (task.state, task.worker_id) == (WorkerTaskState.SUCCEEDED, "worker_fake")
    assert not await env.tasks.has_pending_work()


async def test_failed_attempts_back_off_then_succeed(env):
    env.workers.ready = True
    env.stub.fail_creates = 2
    conn = await _create(env)

    for _ in range(3):
        await env.tasks.run_once()

    assert env.stub.creates == 3
    done = await env.svc.conn_repo.get_connection(conn.id)
    assert done.state == ConnectionState.PROVISIONED


async def test_exhausting_attempts_fails_the_connection_and_frees_the_slot(env):
    env.workers.ready = True
    env.stub.fail_creates = 99
    before = await env.slots()
    conn = await _create(env)

    for _ in range(5):
        await env.tasks.run_once()

    failed = await env.svc.conn_repo.get_connection(conn.id)
    assert (failed.state, failed.status) == (ConnectionState.PENDING, Status.FAILED)
    assert "gave up after 3 attempts" in failed.status_detail
    assert await env.slots() == before  # reserved, then handed back exactly once
    task = await env.task_repo.get_for_connection(conn.id)
    assert (task.state, task.attempts) == (WorkerTaskState.FAILED, 3)


async def test_timing_out_waiting_for_a_worker_fails_and_frees_the_slot(env):
    env.settings.task_timeout_seconds = -1
    before = await env.slots()
    conn = await _create(env)

    await env.tasks.run_once()

    failed = await env.svc.conn_repo.get_connection(conn.id)
    assert failed.status == Status.FAILED
    assert "no worker became ready" in failed.status_detail
    assert await env.slots() == before


async def test_unprovisionable_worker_fails_fast_instead_of_waiting(env):
    env.workers.unavailable = WorkerUnavailableError("circuit breaker open")
    before = await env.slots()
    conn = await _create(env)  # enqueue tolerates it...

    await env.tasks.run_once()  # ...the runner reports it

    failed = await env.svc.conn_repo.get_connection(conn.id)
    assert failed.status == Status.FAILED
    assert "circuit breaker open" in failed.status_detail
    assert await env.slots() == before


# -- terminate / delete ------------------------------------------------------


async def test_terminating_a_pending_connection_cancels_it_and_frees_the_slot(env):
    before = await env.slots()
    conn = await _create(env)

    closed = await env.svc.terminate_connection(conn.id)

    assert closed.state == ConnectionState.CLOSED
    assert closed.closed_at is not None
    assert await env.slots() == before
    task = await env.task_repo.get_for_connection(conn.id)
    assert task.state == WorkerTaskState.CANCELLED
    env.workers.ready = True
    await env.tasks.run_once()
    assert env.stub.creates == 0  # the worker is never asked


async def test_terminating_a_failed_pending_connection_does_not_free_a_slot_twice(env):
    env.settings.task_timeout_seconds = -1
    before = await env.slots()
    conn = await _create(env)
    await env.tasks.run_once()  # fails, slot returned
    assert await env.slots() == before

    closed = await env.svc.terminate_connection(conn.id)

    assert closed.state == ConnectionState.CLOSED
    assert await env.slots() == before


async def test_a_failed_pending_connection_can_be_deleted(env):
    env.settings.task_timeout_seconds = -1
    conn = await _create(env)
    await env.tasks.run_once()

    await env.svc.delete_connection(conn.id)

    assert await env.svc.conn_repo.get_connection(conn.id) is None


async def test_a_waiting_pending_connection_cannot_be_deleted(env):
    from cloud_api.service.connection_service import ConnectionStateError

    conn = await _create(env)

    with pytest.raises(ConnectionStateError):
        await env.svc.delete_connection(conn.id)


async def test_terminated_while_being_created_is_removed_from_the_worker(env):
    env.workers.ready = True
    conn = await _create(env)

    async def terminate_midway(_req):
        await env.svc.conn_repo.close_pending(conn.id)

    env.stub.before_create = terminate_midway

    await env.tasks.run_once()

    assert conn.id not in env.stub.connections  # no orphan left on the worker
    assert (await env.svc.conn_repo.get_connection(conn.id)).state == (
        ConnectionState.CLOSED
    )
    assert (await env.task_repo.get_for_connection(conn.id)).state == (
        WorkerTaskState.CANCELLED
    )


# -- sync guards --------------------------------------------------------------


async def test_sync_does_nothing_without_a_worker(env):
    env.workers.ready = True
    conn = await _create(env)
    await env.tasks.run_once()  # provisioned on the worker
    env.workers.ready = False  # VM torn down / not up

    await env.svc._sync_connections()

    still = await env.svc.conn_repo.get_connection(conn.id)
    assert still.state == ConnectionState.PROVISIONED  # not read as "gone"


async def test_sync_leaves_pending_connections_alone(env):
    # A connection already running on the worker (the free plan has a single
    # slot, so it is seeded directly rather than through the queue).
    other = await env.svc.conn_repo.create_connection(
        Connection(
            name="other",
            node_id=env.tenant.node_id,
            state=ConnectionState.PROVISIONED,
            status=Status.SUCCEEDED,
        )
    )
    env.stub.connections[other.id] = pb2.Connection(
        id=other.id,
        node_id=env.tenant.node_id,
        name="other",
        state="provisioned",
        status="succeeded",
    )
    pending = await _create(env, "queued")  # never reached the worker
    env.workers.ready = True

    await env.svc._sync_connections()

    assert (await env.svc.conn_repo.get_connection(pending.id)).state == (
        ConnectionState.PENDING
    )
    assert (await env.svc.conn_repo.get_connection(other.id)).state == (
        ConnectionState.PROVISIONED
    )


async def test_connection_activated_after_a_sync_snapshot_is_not_seen_as_gone(env):
    """The snapshot of the worker's connections is taken before the runner
    creates this one, but the row was created (as pending) long before that."""
    env.workers.ready = True
    conn = await _create(env)
    snapshot_at = datetime.now(UTC)

    await env.tasks.run_once()  # activates it after the snapshot

    assert conn.id not in await env.svc.conn_repo.list_open_ids(
        created_before=snapshot_at
    )


async def test_pending_failed_connections_do_not_count_as_open(env):
    env.settings.task_timeout_seconds = -1
    principal = env.tenant.principal_id
    base = await env.svc.conn_repo.count_open_for_principal(principal)
    conn = await _create(env)
    assert await env.svc.conn_repo.count_open_for_principal(principal) == base + 1

    await env.tasks.run_once()  # fails -> slot already returned

    assert await env.svc.conn_repo.count_open_for_principal(principal) == base
    assert (await env.svc.conn_repo.get_connection(conn.id)).status == Status.FAILED


# -- streams -----------------------------------------------------------------


async def test_streams_cannot_open_on_a_pending_connection(env):
    from cloud_api import service_manager as sm

    conn = await _create(env)
    streams = sm.get_stream_service()

    with pytest.raises(ConnectionNotReadyError):
        await streams.create_stream(
            NewStreamRequest(connection_id=conn.id, type="gateway", port=8080)
        )


# -- watching ----------------------------------------------------------------


async def test_watch_reports_each_change_then_ends(env):
    conn = await _create(env)
    seen: list[tuple] = []

    async def watch():
        async for c in env.svc.watch_connection(conn.id, poll_seconds=0.05):
            if c is not None:
                seen.append((c.state, c.status, c.status_detail))

    watcher = asyncio.create_task(watch())
    await asyncio.sleep(0.15)
    await env.tasks.run_once()  # no worker: detail changes
    await asyncio.sleep(0.15)
    env.workers.ready = True
    await env.tasks.run_once()  # created
    await asyncio.wait_for(watcher, timeout=5)  # ends once provisioned

    assert seen[0] == (ConnectionState.PENDING, Status.IN_PROGRESS, "queued")
    assert (
        ConnectionState.PENDING,
        Status.IN_PROGRESS,
        "waiting for a worker to start",
    ) in seen
    assert seen[-1] == (ConnectionState.PROVISIONED, Status.SUCCEEDED, None)
    assert len(seen) == len(set(seen))  # only changes are reported


async def test_watch_ends_when_the_connection_fails(env):
    env.settings.task_timeout_seconds = -1
    conn = await _create(env)
    await env.tasks.run_once()

    seen = [c async for c in env.svc.watch_connection(conn.id, poll_seconds=0.05) if c]

    assert [(c.state, c.status) for c in seen] == [
        (ConnectionState.PENDING, Status.FAILED)
    ]


# -- HTTP surface -------------------------------------------------------------


def _http(principal_id: str):
    import httpx

    import cloud_api
    from cloud_api import service_manager as sm
    from cloud_api.interface.router import app

    token = sm.get_auth_lib().mint_access_token(
        user_id=f"user_{principal_id}", principal_account_id=principal_id
    )
    return httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app),
        base_url=f"http://test{cloud_api.app_config.api.base_path}",
        headers={"Authorization": f"Bearer {token}"},
    )


@pytest_asyncio.fixture
async def http_env(env):
    from cloud_api import service_manager as sm
    from cloud_api.interface.router import app

    app.dependency_overrides[sm.get_connection_service] = lambda: env.svc
    yield env
    app.dependency_overrides.pop(sm.get_connection_service, None)


async def test_post_returns_202_while_waiting_for_a_worker(http_env):
    env = http_env
    async with _http(env.tenant.principal_id) as c:
        resp = await c.post(
            "/connections", json={"name": "web", "node_id": env.tenant.node_id}
        )

        assert resp.status_code == 202
        body = resp.json()
        assert body["state"] == "pending"
        assert body["status"] == "in_progress"
        # Still readable (and owned-checked) through the normal GET.
        got = await c.get(f"/connections/{body['id']}")
        assert got.status_code == 200
        assert got.json()["state"] == "pending"


async def test_events_endpoint_streams_progress_until_provisioned(http_env):
    env = http_env
    conn = await _create(env)

    async with _http(env.tenant.principal_id) as c:
        stream = asyncio.create_task(c.get(f"/connections/{conn.id}/events"))
        await asyncio.sleep(0.2)
        env.workers.ready = True
        await env.tasks.run_once()
        resp = await asyncio.wait_for(stream, timeout=10)

    assert resp.status_code == 200
    assert resp.headers["content-type"].startswith("text/event-stream")
    events = [
        line.removeprefix("data: ")
        for line in resp.text.splitlines()
        if line.startswith("data: ")
    ]
    import json

    states = [(json.loads(e)["state"], json.loads(e)["status"]) for e in events]
    assert states[0] == ("pending", "in_progress")
    assert states[-1] == ("provisioned", "succeeded")
    assert all(
        line.startswith(("event: ", "data: ", ":")) or not line
        for line in resp.text.splitlines()
    )


async def test_events_endpoint_hides_other_tenants_connections(http_env, other_tenant):
    conn = await _create(http_env)

    async with _http(other_tenant.principal_id) as c:
        resp = await c.get(f"/connections/{conn.id}/events")

    assert resp.status_code == 404
