"""WorkerService end to end against real Postgres, a FakeVMManager and a fake
gRPC client: the whole state machine, the VM callbacks, failure handling,
idle teardown and orphan reaping. Skipped when the migrated schema is absent."""

import asyncio
import re
from datetime import UTC, datetime, timedelta

import httpx
import pytest
import pytest_asyncio
from cryptography import x509
from sqlalchemy import text

import cloud_api
from cloud_api.config import WorkerDefaults
from cloud_api.interface.router import app
from cloud_api.lib.dns import FakeDNSManager
from cloud_api.lib.events import EventBus
from cloud_api.lib.vm_manager import FakeVMManager, VMSpec, VMStatus
from cloud_api.models.api.common import Status
from cloud_api.models.api.worker import WorkerState, WorkerType
from cloud_api.repository import session as db_session
from cloud_api.repository.worker_event_repository import WorkerEventRepository
from cloud_api.repository.worker_repository import WorkerRepository
from cloud_api.service import worker_service as ws
from cloud_api.service.worker_service import (
    InvalidWorkerTokenError,
    WorkerService,
    WorkerUnavailableError,
)
from tests.conftest import generate_csr_pem

pytestmark = pytest.mark.asyncio

T = WorkerType.CONNECTION_WORKER
BASE = cloud_api.app_config.api.base_path

S = (WorkerState, Status)


class FakeClient:
    def __init__(self):
        self.up = True
        self.connections: list[str] = []
        self.closed = False

    async def ping(self, timeout: float = 5.0) -> bool:
        return self.up

    async def list_connections(self):
        return list(self.connections)

    async def close(self):
        self.closed = True


def make_settings(**overrides) -> WorkerDefaults:
    base = {
        "image": "ghcr.io/acme/conn-worker:1",
        "api_public_url": "https://api.test/api/v2/cloud",
        "idle_grace_seconds": 600,
        "drain_seconds": 30,
        "boot_timeout_seconds": 600,
        "unhealthy_grace_seconds": 120,
        "max_provision_attempts": 3,
        "breaker_threshold": 3,
    }
    return WorkerDefaults(**{**base, **overrides})


class Env:
    def __init__(self, cert_lib, **settings):
        self.dns = FakeDNSManager()
        self.vms = FakeVMManager()
        self.client = FakeClient()
        self.pending = False
        self.repo = WorkerRepository()
        self.events = WorkerEventRepository()
        self.svc = WorkerService(
            worker_repository=self.repo,
            event_repository=self.events,
            vm_manager=self.vms,
            cert_lib=cert_lib,
            settings=make_settings(**settings),
            event_bus=EventBus(),
            has_pending_work=self._pending,
            dns_manager=self.dns,
        )

        async def get_client(worker):
            return self.client

        self.svc.get_client = get_client

    async def _pending(self) -> bool:
        return self.pending

    async def tick(self, n: int = 1):
        for _ in range(n):
            await self.svc.reconcile()

    async def worker(self, worker_id: str):
        return await self.repo.get_worker(worker_id)

    async def pair(self, worker_id: str):
        w = await self.worker(worker_id)
        return (w.state, w.status)

    async def backdate(self, worker_id: str, seconds: float, **extra):
        """Pretend the worker has been in its current state for `seconds` longer."""
        w = await self.worker(worker_id)
        await self.repo.update_worker(
            worker_id,
            state_changed_at=w.state_changed_at - timedelta(seconds=seconds),
            **({"extra_patch": extra} if extra else {}),
        )

    def token(self) -> str:
        user_data = next(iter(self.vms.user_data.values()))
        return re.search(r'token: "([^"]+)"', user_data).group(1)

    async def boot(self) -> str:
        """Drive a worker all the way to AVAILABLE + SUCCEEDED."""
        w = await self.svc.request_worker(T)
        await self.tick(2)  # create VM; await bootstrap
        await self.svc.register_worker(w.id, self.token(), generate_csr_pem())
        await self.svc.mark_ready(w.id, self.token())
        await self.tick(2)  # verify; ready
        assert await self.pair(w.id) == (WorkerState.AVAILABLE, Status.SUCCEEDED)
        return w.id


@pytest_asyncio.fixture
async def env(db, cert_lib):
    async with db_session.get_session() as s:
        try:
            await s.execute(text("DELETE FROM underleaf.worker_events"))
            await s.execute(text("DELETE FROM underleaf.workers"))
            await s.commit()
        except Exception as exc:  # noqa: BLE001
            pytest.skip(f"worker tables not migrated: {exc}")
    return Env(cert_lib)


# ---------------------------------------------------------------- demand


async def test_concurrent_requests_make_one_worker_and_no_vm_yet(env):
    workers = await asyncio.gather(*(env.svc.request_worker(T) for _ in range(6)))
    assert len({w.id for w in workers}) == 1
    assert env.vms.vms == {}  # request never provisions; the reconciler does
    assert (workers[0].state, workers[0].status) == (
        WorkerState.REQUESTED,
        Status.SUCCEEDED,
    )


@pytest.mark.parametrize(
    "overrides",
    [{"provisioning_enabled": False}, {"image": ""}, {"api_public_url": ""}],
    ids=["disabled", "no-image", "no-callback-url"],
)
async def test_request_refused_when_it_could_not_work(db, cert_lib, overrides):
    e = Env(cert_lib, **overrides)
    async with db_session.get_session() as s:
        await s.execute(text("DELETE FROM underleaf.workers"))
        await s.commit()
    with pytest.raises(WorkerUnavailableError):
        await e.svc.request_worker(T)


async def test_kill_switch_parks_a_requested_worker(env):
    w = await env.svc.request_worker(T)
    env.svc.settings.provisioning_enabled = False
    await env.tick(3)
    assert env.vms.vms == {}
    assert await env.pair(w.id) == (WorkerState.REQUESTED, Status.SUCCEEDED)


# ---------------------------------------------------------------- happy path


async def test_full_lifecycle_to_ready(env):
    worker_id = await env.boot()

    (vm,) = env.vms.vms.values()
    assert vm.tags["worker_id"] == worker_id and vm.tags["app"] == "underleaf"
    assert "ghcr.io/acme/conn-worker:1" in env.vms.user_data[vm.provider_id]

    w = await env.svc.wait_ready(worker_id, timeout=1)
    assert w.is_ready and w.extra_data["vm"]["public_ip"] == vm.public_ip
    assert "bootstrap_token_hash" in w.extra_data and env.token() not in str(
        w.extra_data
    )

    types = [e.type for e in await env.events.list_events(worker_id=worker_id)]
    expected = [
        "worker.requested",
        "worker.creating",
        "worker.created",
        "worker.awaiting_bootstrap",
        "worker.registered",
        "worker.configured",
        "worker.verifying",
        "worker.ready",
    ]
    it = iter(types)
    assert all(t in it for t in expected), types  # in order, others allowed between


async def test_get_ready_worker_only_when_ready(env):
    assert await env.svc.get_ready_worker(T) is None
    w = await env.svc.request_worker(T)
    assert await env.svc.get_ready_worker(T) is None
    worker_id = (await env.repo.get_live_worker(T)).id
    assert worker_id == w.id
    await env.repo.update_worker(
        w.id, state=WorkerState.AVAILABLE, status=Status.SUCCEEDED
    )
    assert (await env.svc.get_ready_worker(T)).id == w.id


async def test_wait_ready_wakes_on_event_and_times_out(env):
    w = await env.svc.request_worker(T)
    with pytest.raises(TimeoutError):
        await env.svc.wait_ready(w.id, timeout=0.05)

    waiter = asyncio.create_task(env.svc.wait_ready(w.id, timeout=5))
    await asyncio.sleep(0.05)
    await env.tick(2)
    await env.svc.register_worker(w.id, env.token(), generate_csr_pem())
    await env.svc.mark_ready(w.id, env.token())
    await env.tick(2)
    assert (await asyncio.wait_for(waiter, 3)).is_ready


async def test_wait_ready_raises_if_worker_is_going_away(env):
    w = await env.svc.request_worker(T)
    await env.svc.destroy_worker(w.id)
    with pytest.raises(WorkerUnavailableError):
        await env.svc.wait_ready(w.id, timeout=1)


# ---------------------------------------------------------------- VM callbacks


async def test_register_issues_cert_for_the_vm_ip_only(env):
    w = await env.svc.request_worker(T)
    await env.tick(2)
    (vm,) = env.vms.vms.values()

    cert_pem, ca = await env.svc.register_worker(w.id, env.token(), generate_csr_pem())
    san = x509.load_pem_x509_certificate(
        cert_pem.encode()
    ).extensions.get_extension_for_class(x509.SubjectAlternativeName)
    assert [str(i) for i in san.value.get_values_for_type(x509.IPAddress)] == [
        vm.public_ip
    ]
    assert ca == env.svc.cert_lib.ca_pem


async def test_bad_token_unknown_worker_and_dead_worker_are_rejected(env):
    w = await env.svc.request_worker(T)
    await env.tick(2)
    good = env.token()
    csr = generate_csr_pem()

    with pytest.raises(InvalidWorkerTokenError):
        await env.svc.register_worker(w.id, "wrong", csr)
    with pytest.raises(InvalidWorkerTokenError):
        await env.svc.register_worker("worker_nope", good, csr)
    with pytest.raises(InvalidWorkerTokenError):
        await env.svc.mark_ready(w.id, "wrong")

    await env.svc.destroy_worker(w.id)
    with pytest.raises(InvalidWorkerTokenError):
        await env.svc.register_worker(w.id, good, csr)


async def test_ready_callback_is_idempotent(env):
    worker_id = await env.boot()
    token = env.token()
    again = await env.svc.mark_ready(worker_id, token)
    assert again.state == WorkerState.AVAILABLE
    assert await env.pair(worker_id) == (WorkerState.AVAILABLE, Status.SUCCEEDED)


async def test_lost_ready_callback_is_caught_by_the_health_check(env):
    w = await env.svc.request_worker(T)
    await env.tick(2)
    await env.svc.register_worker(w.id, env.token(), generate_csr_pem())
    # no mark_ready: the VM's final POST was lost
    await env.tick(1)
    assert await env.pair(w.id) == (WorkerState.CONFIGURED, Status.SUCCEEDED)


async def test_slow_boot_ip_is_filled_in_later(env):
    env.vms.create_status = VMStatus.PENDING
    w = await env.svc.request_worker(T)
    await env.tick(3)
    assert (
        "public_ip" not in (await env.worker(w.id)).extra_data["vm"]
        or not (await env.worker(w.id)).extra_data["vm"]["public_ip"]
    )
    with pytest.raises(
        InvalidWorkerTokenError
    ):  # can't certify a VM with no address yet
        await env.svc.register_worker(w.id, env.token(), generate_csr_pem())

    (vm,) = env.vms.vms.values()
    env.vms.activate(vm.provider_id)
    await env.tick(1)
    assert (await env.worker(w.id)).extra_data["vm"][
        "public_ip"
    ] == f"203.0.113.{vm.provider_id[5:]}"
    await env.svc.register_worker(w.id, env.token(), generate_csr_pem())


# ---------------------------------------------------------------- failure


async def test_vm_create_failures_retry_then_give_up_and_trip_the_breaker(db, cert_lib):
    async with db_session.get_session() as s:
        await s.execute(text("DELETE FROM underleaf.workers"))
        await s.commit()
    e = Env(cert_lib, max_provision_attempts=2, breaker_threshold=1)

    async def boom(*a, **k):
        raise RuntimeError("quota exceeded")

    e.vms.create_vm = boom
    w = await e.svc.request_worker(T)

    await e.tick()  # attempt 1 fails
    assert await e.pair(w.id) == (WorkerState.CREATED, Status.FAILED)
    await e.tick()  # still inside the backoff
    assert await e.pair(w.id) == (WorkerState.CREATED, Status.FAILED)

    await e.backdate(w.id, 100)
    await e.tick()  # backoff over -> retry
    assert await e.pair(w.id) == (WorkerState.REQUESTED, Status.SUCCEEDED)
    assert (await e.worker(w.id)).error is None

    await e.tick()  # attempt 2 fails
    await e.backdate(w.id, 100)
    await e.tick()  # out of attempts -> teardown
    assert await e.pair(w.id) == (WorkerState.DELETED, Status.IN_PROGRESS)
    await e.tick()
    # Gone is DELETED + SUCCEEDED; the failure lives on in `error`.
    assert await e.pair(w.id) == (WorkerState.DELETED, Status.SUCCEEDED)
    assert "quota exceeded" in (await e.worker(w.id)).error

    with pytest.raises(WorkerUnavailableError, match="circuit breaker"):
        await e.svc.request_worker(T)


async def test_boot_timeout_deletes_the_vm_and_starts_over(env):
    w = await env.svc.request_worker(T)
    await env.tick(3)  # VM up, nobody ever calls back
    (old_vm,) = env.vms.vms.values()
    assert await env.pair(w.id) == (WorkerState.CONFIGURED, Status.IN_PROGRESS)

    long_ago = (datetime.now(UTC) - timedelta(seconds=700)).isoformat()
    await env.backdate(w.id, 0, booting_since=long_ago)
    await env.tick()
    assert await env.pair(w.id) == (WorkerState.CONFIGURED, Status.FAILED)

    await env.backdate(w.id, 100)
    await env.tick()  # retry: old VM is deleted before a new attempt
    assert old_vm.provider_id not in env.vms.vms
    assert await env.pair(w.id) == (WorkerState.REQUESTED, Status.SUCCEEDED)

    await env.tick()
    (new_vm,) = env.vms.vms.values()
    assert new_vm.provider_id != old_vm.provider_id


async def test_unreachable_worker_is_replaced_after_the_grace(env):
    worker_id = await env.boot()
    env.client.up = False
    await env.tick()  # starts the unhealthy clock
    assert (await env.worker(worker_id)).extra_data["unhealthy_since"]
    assert await env.pair(worker_id) == (WorkerState.AVAILABLE, Status.SUCCEEDED)

    past = (datetime.now(UTC) - timedelta(seconds=200)).isoformat()
    await env.backdate(worker_id, 0, unhealthy_since=past)
    await env.tick()
    assert await env.pair(worker_id) == (WorkerState.AVAILABLE, Status.FAILED)
    await env.tick()  # not retryable once it has served: tear down
    assert await env.pair(worker_id) == (WorkerState.DELETED, Status.IN_PROGRESS)
    await env.tick()
    assert await env.pair(worker_id) == (WorkerState.DELETED, Status.SUCCEEDED)
    assert (await env.worker(worker_id)).error
    assert env.vms.vms == {}


async def test_recovery_clears_the_unhealthy_clock(env):
    worker_id = await env.boot()
    env.client.up = False
    await env.tick()
    env.client.up = True
    await env.tick()
    assert "unhealthy_since" not in (await env.worker(worker_id)).extra_data


# ---------------------------------------------------------------- idle teardown


async def test_idle_worker_is_drained_then_deleted_then_replaceable(env):
    worker_id = await env.boot()

    await env.tick()  # first idle observation starts the clock
    assert (await env.worker(worker_id)).extra_data["idle_since"]
    assert await env.pair(worker_id) == (WorkerState.AVAILABLE, Status.SUCCEEDED)

    past = (datetime.now(UTC) - timedelta(seconds=700)).isoformat()
    await env.backdate(worker_id, 0, idle_since=past)
    await env.tick()
    assert await env.pair(worker_id) == (WorkerState.DRAINING, Status.IN_PROGRESS)

    await env.tick()  # drain period not over: VM still there
    assert await env.pair(worker_id) == (WorkerState.DRAINING, Status.IN_PROGRESS)
    assert len(env.vms.vms) == 1

    await env.backdate(worker_id, 100)
    await env.tick()  # drain period over: drained
    assert await env.pair(worker_id) == (WorkerState.DRAINING, Status.SUCCEEDED)
    await env.tick()  # teardown begins; the VM still exists and still holds the slot
    assert await env.pair(worker_id) == (WorkerState.DELETED, Status.IN_PROGRESS)
    assert len(env.vms.vms) == 1
    await env.tick()
    assert await env.pair(worker_id) == (WorkerState.DELETED, Status.SUCCEEDED)
    assert env.vms.vms == {}

    fresh = await env.svc.request_worker(T)  # the slot is free again
    assert fresh.id != worker_id


async def test_open_connections_or_queued_work_keep_a_worker_alive(env):
    worker_id = await env.boot()
    past = (datetime.now(UTC) - timedelta(seconds=700)).isoformat()

    env.client.connections = ["conn_1"]
    await env.backdate(worker_id, 0, idle_since=past)
    await env.tick()
    assert await env.pair(worker_id) == (WorkerState.AVAILABLE, Status.SUCCEEDED)
    assert "idle_since" not in (await env.worker(worker_id)).extra_data

    env.client.connections = []
    env.pending = True
    await env.backdate(worker_id, 0, idle_since=past)
    await env.tick()
    assert await env.pair(worker_id) == (WorkerState.AVAILABLE, Status.SUCCEEDED)


async def test_new_demand_cancels_a_drain(env):
    worker_id = await env.boot()
    past = (datetime.now(UTC) - timedelta(seconds=700)).isoformat()
    await env.backdate(worker_id, 0, idle_since=past)
    await env.tick(2)
    assert (await env.worker(worker_id)).state == WorkerState.DRAINING

    w = await env.svc.request_worker(T)
    assert w.id == worker_id and w.is_ready
    await env.backdate(worker_id, 100)
    await env.tick()
    assert await env.pair(worker_id) == (WorkerState.AVAILABLE, Status.SUCCEEDED)
    assert len(env.vms.vms) == 1


async def test_max_lifetime_drains_even_a_busy_worker(db, cert_lib):
    async with db_session.get_session() as s:
        await s.execute(text("DELETE FROM underleaf.workers"))
        await s.commit()
    e = Env(cert_lib, max_vm_lifetime_hours=1)
    worker_id = await e.boot()
    e.client.connections = ["conn_1"]
    long_ago = (datetime.now(UTC) - timedelta(hours=2)).isoformat()
    await e.backdate(worker_id, 0, booting_since=long_ago)
    await e.tick()
    assert (await e.worker(worker_id)).state == WorkerState.DRAINING


# ---------------------------------------------------------------- teardown / orphans


async def test_destroy_from_any_state_deletes_the_vm(env):
    w = await env.svc.request_worker(T)
    await env.tick(2)
    assert len(env.vms.vms) == 1
    await env.svc.destroy_worker(w.id)
    await env.tick()
    assert await env.pair(w.id) == (WorkerState.DELETED, Status.SUCCEEDED)
    assert env.vms.vms == {}
    with pytest.raises(KeyError):
        await env.svc.destroy_worker("worker_nope")


async def test_delete_failure_keeps_retrying_instead_of_leaking(env):
    w = await env.svc.request_worker(T)
    await env.tick(2)
    await env.svc.destroy_worker(w.id)

    real = env.vms.delete_vm

    async def flaky(provider_id):
        raise RuntimeError("provider 500")

    env.vms.delete_vm = flaky
    await env.tick()
    assert await env.pair(w.id) == (WorkerState.DELETED, Status.IN_PROGRESS)
    assert len(env.vms.vms) == 1

    env.vms.delete_vm = real
    await env.tick()
    assert await env.pair(w.id) == (WorkerState.DELETED, Status.SUCCEEDED)
    assert env.vms.vms == {}


async def test_existing_tagged_vm_is_adopted_not_duplicated(env):
    w = await env.svc.request_worker(T)
    # a previous attempt created the VM, then we crashed before recording it
    pre = await env.vms.create_vm(
        "h",
        VMSpec(cpus=2, mem_gb=4, storage_gb=20),
        tags={**ws.TAG_APP, "env": "prod", "worker_id": w.id},
    )
    await env.tick()
    assert len(env.vms.vms) == 1
    assert (await env.worker(w.id)).extra_data["vm"]["provider_id"] == pre.provider_id


async def test_orphan_reaper_keeps_live_and_young_vms(env):
    spec = VMSpec(cpus=2, mem_gb=4, storage_gb=20)
    tags = {**ws.TAG_APP, "env": "prod"}

    w = await env.svc.request_worker(T)
    await env.tick(1)  # live worker's VM
    live_vm = next(iter(env.vms.vms.values()))

    old = await env.vms.create_vm(
        "old", spec, tags={**tags, "worker_id": "worker_gone"}
    )
    env.vms.vms[old.provider_id] = old.model_copy(
        update={"created_at": datetime.now(UTC) - timedelta(hours=1)}
    )
    young = await env.vms.create_vm(
        "young", spec, tags={**tags, "worker_id": "worker_gone"}
    )
    other_env = await env.vms.create_vm(
        "x", spec, tags={**ws.TAG_APP, "env": "dev", "worker_id": "w"}
    )
    env.vms.vms[other_env.provider_id] = other_env.model_copy(
        update={"created_at": datetime.now(UTC) - timedelta(hours=1)}
    )

    reaped = await env.svc.reap_orphans()
    assert [v.provider_id for v in reaped] == [old.provider_id]
    assert set(env.vms.vms) == {
        live_vm.provider_id,
        young.provider_id,
        other_env.provider_id,
    }
    assert w.id  # live worker untouched


# ---------------------------------------------------------------- HTTP callbacks


async def test_http_callbacks_authenticate_by_token(env):
    from cloud_api.service_manager import get_worker_service

    app.dependency_overrides[get_worker_service] = lambda: env.svc
    try:
        w = await env.svc.request_worker(T)
        await env.tick(2)
        token = env.token()
        csr = generate_csr_pem()
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=app), base_url=f"http://test{BASE}"
        ) as http:
            url = f"/internal/workers/{w.id}"
            bad = await http.post(f"{url}/register", json={"token": "nope", "csr": csr})
            assert bad.status_code == 401
            assert (
                await http.post(
                    "/internal/workers/worker_x/ready", json={"token": token}
                )
            ).status_code == 401

            ok = await http.post(f"{url}/register", json={"token": token, "csr": csr})
            assert (
                ok.status_code == 200
                and "BEGIN CERTIFICATE" in ok.json()["certificate"]
            )
            garbage = await http.post(
                f"{url}/register", json={"token": token, "csr": "not a csr"}
            )
            assert garbage.status_code in (401, 422, 500) and garbage.status_code != 200

            ready = await http.post(f"{url}/ready", json={"token": token})
            assert ready.status_code == 204
        assert await env.pair(w.id) == (WorkerState.CONFIGURED, Status.SUCCEEDED)
    finally:
        app.dependency_overrides.pop(get_worker_service, None)


# ---------------------------------------------------------------- DNS


async def test_dns_points_at_the_vm_once_ready_and_is_removed_with_it(db, cert_lib):
    e = Env(
        cert_lib, tunnel_hostname="tunnel.example.com", dns_names=["*.gw.example.com"]
    )
    await _clean_workers()

    worker_id = await e.boot()

    ip = (await e.worker(worker_id)).extra_data["vm"]["public_ip"]
    assert e.dns.records == {"tunnel.example.com": ip, "*.gw.example.com": ip}

    await e.svc.destroy_worker(worker_id)
    await e.tick(2)

    assert e.dns.records == {}


async def test_dns_failure_keeps_the_worker_unready_then_recovers(db, cert_lib):
    e = Env(cert_lib, tunnel_hostname="tunnel.example.com")
    await _clean_workers()
    w = await e.svc.request_worker(T)
    await e.tick(2)
    await e.svc.register_worker(w.id, e.token(), generate_csr_pem())
    await e.svc.mark_ready(w.id, e.token())
    e.dns.fail_next_upsert = RuntimeError("cloudflare down")

    await e.tick(2)  # first attempt fails; agents couldn't find it yet

    assert await e.pair(w.id) == (WorkerState.AVAILABLE, Status.IN_PROGRESS)
    assert e.dns.records == {}

    await e.tick(1)  # next tick succeeds

    assert await e.pair(w.id) == (WorkerState.AVAILABLE, Status.SUCCEEDED)
    assert "tunnel.example.com" in e.dns.records


async def test_a_replaced_workers_teardown_leaves_the_new_workers_dns_alone(
    db, cert_lib
):
    e = Env(cert_lib, tunnel_hostname="tunnel.example.com")
    await _clean_workers()
    old_id = await e.boot()
    old = await e.worker(old_id)
    e.dns.records["tunnel.example.com"] = "198.51.100.77"  # a newer worker took it

    await e.svc.destroy_worker(old.id)
    await e.tick(2)

    assert e.dns.records == {"tunnel.example.com": "198.51.100.77"}


async def _clean_workers():
    async with db_session.get_session() as s:
        await s.execute(text("DELETE FROM underleaf.worker_events"))
        await s.execute(text("DELETE FROM underleaf.workers"))
        await s.commit()
