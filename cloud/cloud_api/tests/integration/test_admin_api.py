"""The operator API and its reports against real Postgres: access control,
worker/task/VM operations, persisted settings overrides, and the cost and
cold-start rollups."""

import asyncio
from datetime import UTC, datetime, timedelta
from types import SimpleNamespace

import httpx
import pytest
import pytest_asyncio
from sqlalchemy import text

import cloud_api
from cloud_api import service_manager as sm
from cloud_api.config import WorkerDefaults
from cloud_api.interface.router import app
from cloud_api.lib.vm_manager import VMSpec
from cloud_api.models.api import Connection, ConnectionState, Status
from cloud_api.models.api.worker import (
    WorkerState,
    WorkerTaskKind,
    WorkerTaskState,
    WorkerType,
)
from cloud_api.repository import session as db_session
from cloud_api.repository.worker_event_repository import WorkerEventRepository
from cloud_api.repository.worker_repository import WorkerRepository
from cloud_api.repository.worker_settings_repository import WorkerSettingsRepository
from cloud_api.repository.worker_task_repository import WorkerTaskRepository
from cloud_api.service.worker_report_service import WorkerReportService
from cloud_api.service.worker_settings_service import (
    InvalidSettingError,
    WorkerSettingsService,
)
from tests.integration.test_worker_lifecycle import Env

pytestmark = pytest.mark.asyncio

T = WorkerType.CONNECTION_WORKER


async def _wipe():
    async with db_session.get_session() as s:
        await s.execute(text("DELETE FROM underleaf.worker_settings"))
        await s.execute(text("DELETE FROM underleaf.worker_events"))
        await s.execute(text("DELETE FROM underleaf.workers"))
        await s.execute(
            text(
                "UPDATE underleaf.worker_tasks SET state='cancelled' "
                "WHERE state IN ('queued','running')"
            )
        )
        await s.commit()


@pytest_asyncio.fixture
async def admin(db, cert_lib, tenant, other_tenant, monkeypatch):
    await _wipe()
    monkeypatch.setattr(
        cloud_api.app_config.admin, "principal_account_ids", [tenant.principal_id]
    )
    env = Env(cert_lib, hourly_cost_usd=0.5)
    settings = WorkerSettingsService(WorkerSettingsRepository(), WorkerDefaults())
    app.dependency_overrides[sm.get_worker_service] = lambda: env.svc
    app.dependency_overrides[sm.get_worker_settings_service] = lambda: settings
    app.dependency_overrides[sm.get_worker_report_service] = lambda: _reports()

    def client(principal_id: str) -> httpx.AsyncClient:
        token = sm.get_auth_lib().mint_access_token(
            user_id=f"user_{principal_id}", principal_account_id=principal_id
        )
        return httpx.AsyncClient(
            transport=httpx.ASGITransport(app=app),
            base_url=f"http://test{cloud_api.app_config.api.base_path}",
            headers={"Authorization": f"Bearer {token}"},
        )

    yield SimpleNamespace(
        env=env,
        settings=settings,
        tenant=tenant,
        op=client(tenant.principal_id),
        outsider=client(other_tenant.principal_id),
    )
    app.dependency_overrides.pop(sm.get_worker_service, None)
    app.dependency_overrides.pop(sm.get_worker_settings_service, None)
    app.dependency_overrides.pop(sm.get_worker_report_service, None)
    await env.svc.close()


# -- access ------------------------------------------------------------------


@pytest.mark.parametrize(
    "method,path",
    [
        ("GET", "/admin/overview"),
        ("GET", "/admin/cost"),
        ("GET", "/admin/workers"),
        ("POST", "/admin/workers"),
        ("GET", "/admin/worker-tasks"),
        ("GET", "/admin/vms"),
        ("GET", "/admin/worker-settings"),
        ("PUT", "/admin/worker-settings"),
        ("POST", "/admin/workers/worker_x/destroy"),
        ("GET", "/admin/connections/conn_x/events"),
    ],
)
async def test_non_admins_get_404_everywhere(admin, method, path):
    resp = await admin.outsider.request(method, path, json={})

    assert resp.status_code == 404
    assert (await admin.env.repo.list_live_workers()) == []  # and nothing happened


async def test_admin_api_is_off_when_no_admins_are_configured(admin, monkeypatch):
    monkeypatch.setattr(cloud_api.app_config.admin, "principal_account_ids", [])

    assert (await admin.op.get("/admin/workers")).status_code == 404


async def test_unauthenticated_requests_are_rejected(admin):
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app),
        base_url=f"http://test{cloud_api.app_config.api.base_path}",
    ) as anon:
        assert (await anon.get("/admin/workers")).status_code in (401, 403)


# -- workers -------------------------------------------------------------------


async def test_prewarm_then_list_and_inspect_with_timeline(admin):
    created = await admin.op.post("/admin/workers")
    assert created.status_code == 202
    worker_id = created.json()["id"]

    listed = (await admin.op.get("/admin/workers")).json()
    assert [w["id"] for w in listed["items"]] == [worker_id]
    assert listed["total"] == 1

    detail = (await admin.op.get(f"/admin/workers/{worker_id}")).json()
    assert detail["worker"]["state"] == "REQUESTED"
    assert [e["type"] for e in detail["events"]] == ["worker.requested"]

    assert (await admin.op.get("/admin/workers/worker_nope")).status_code == 404


async def test_prewarm_reports_why_it_cannot(admin):
    admin.env.svc.settings.provisioning_enabled = False

    resp = await admin.op.post("/admin/workers")

    assert resp.status_code == 503
    assert "disabled" in resp.json()["detail"]


async def test_list_filters_by_state(admin):
    await admin.op.post("/admin/workers")

    assert (await admin.op.get("/admin/workers?state=AVAILABLE")).json()["total"] == 0
    assert (await admin.op.get("/admin/workers?state=REQUESTED")).json()["total"] == 1


async def test_drain_only_works_on_a_ready_worker_and_demand_cancels_it(admin):
    worker_id = await admin.env.boot()

    resp = await admin.op.post(f"/admin/workers/{worker_id}/drain")
    assert resp.status_code == 200
    assert resp.json()["state"] == "DRAINING"

    again = await admin.op.post(f"/admin/workers/{worker_id}/drain")
    assert again.status_code == 409  # not ready any more

    await admin.env.svc.request_worker(T)  # new demand
    assert (await admin.env.worker(worker_id)).is_ready


async def test_drain_unknown_worker_is_404(admin):
    assert (await admin.op.post("/admin/workers/worker_nope/drain")).status_code == 404


async def test_destroy_tears_the_vm_down(admin):
    worker_id = await admin.env.boot()
    assert admin.env.vms.vms

    resp = await admin.op.post(f"/admin/workers/{worker_id}/destroy")
    await admin.env.tick(2)

    assert resp.status_code == 200
    assert admin.env.vms.vms == {}
    assert (await admin.env.worker(worker_id)).is_gone


# -- VMs ------------------------------------------------------------------------


async def test_vm_audit_flags_orphans_and_missing_vms(admin):
    worker_id = await admin.env.boot()
    orphan = await admin.env.vms.create_vm(
        "stray",
        VMSpec(cpus=1, mem_gb=1, storage_gb=1),
        tags={**admin.env.svc._tags(), "worker_id": "worker_gone"},
    )

    audit = (await admin.op.get("/admin/vms")).json()

    by_id = {row["vm"]["provider_id"]: row for row in audit["vms"]}
    assert by_id[orphan.provider_id]["orphan"] is True
    owned = [r for r in audit["vms"] if r["worker_id"] == worker_id]
    assert len(owned) == 1 and owned[0]["orphan"] is False

    # The provider loses the real VM behind our back.
    await admin.env.vms.delete_vm(owned[0]["vm"]["provider_id"])
    audit = (await admin.op.get("/admin/vms")).json()
    assert [m["worker_id"] for m in audit["missing"]] == [worker_id]


async def test_vm_delete_refuses_vms_this_deployment_does_not_manage(admin):
    stranger = await admin.env.vms.create_vm(
        "someone-elses", VMSpec(cpus=1, mem_gb=1, storage_gb=1), tags={"app": "other"}
    )
    ours = await admin.env.vms.create_vm(
        "ours",
        VMSpec(cpus=1, mem_gb=1, storage_gb=1),
        tags={**admin.env.svc._tags(), "worker_id": "worker_gone"},
    )

    refused = await admin.op.post(f"/admin/vms/{stranger.provider_id}/delete")
    deleted = await admin.op.post(f"/admin/vms/{ours.provider_id}/delete")

    assert refused.status_code == 404
    assert stranger.provider_id in admin.env.vms.vms
    assert deleted.status_code == 200
    assert ours.provider_id not in admin.env.vms.vms


# -- tasks & connection timeline ------------------------------------------------


async def _pending_connection(admin, task_service=None) -> tuple[Connection, str]:
    conn = await sm.get_conn_repo().create_connection(
        Connection(
            name="q",
            node_id=admin.tenant.node_id,
            state=ConnectionState.PENDING,
            status=Status.IN_PROGRESS,
        )
    )
    task = await WorkerTaskRepository().enqueue(
        WorkerTaskKind.CREATE_CONNECTION, connection_id=conn.id
    )
    return conn, task.id


async def test_cancel_task_terminates_its_pending_connection(admin, monkeypatch):
    conn, task_id = await _pending_connection(admin)
    # Terminating a pending connection never talks to a worker, so the
    # static client is irrelevant here; the service is the real one.
    app.dependency_overrides[sm.get_connection_service] = lambda: (
        sm.get_connection_service()
    )
    try:
        listed = (await admin.op.get("/admin/worker-tasks?state=queued")).json()
        assert task_id in [t["id"] for t in listed]

        resp = await admin.op.post(f"/admin/worker-tasks/{task_id}/cancel")
    finally:
        app.dependency_overrides.pop(sm.get_connection_service, None)

    assert resp.status_code == 200
    assert resp.json()["state"] == WorkerTaskState.CANCELLED.value
    assert (await sm.get_conn_repo().get_connection(conn.id)).state == (
        ConnectionState.CLOSED
    )
    again = await admin.op.post(f"/admin/worker-tasks/{task_id}/cancel")
    assert again.status_code == 409


async def test_connection_timeline_lists_its_events(admin):
    events = WorkerEventRepository()
    await events.append("task.queued", "queued", connection_id="conn_t1")
    await events.append("task.succeeded", "done", connection_id="conn_t1")
    await events.append("task.queued", "other", connection_id="conn_t2")

    resp = await admin.op.get("/admin/connections/conn_t1/events")

    assert [e["type"] for e in resp.json()] == ["task.queued", "task.succeeded"]


# -- settings -------------------------------------------------------------------


async def test_pausing_provisioning_persists_and_clearing_restores(admin):
    put = await admin.op.put(
        "/admin/worker-settings", json={"provisioning_enabled": False}
    )
    assert put.status_code == 200
    assert put.json()["effective"]["provisioning_enabled"] is False
    assert put.json()["overrides"] == {"provisioning_enabled": False}
    assert admin.settings.settings.provisioning_enabled is False

    # A different process (fresh service, same DB) sees it too.
    other = WorkerSettingsService(WorkerSettingsRepository(), WorkerDefaults())
    assert (await other.describe())["effective"]["provisioning_enabled"] is False

    cleared = await admin.op.put(
        "/admin/worker-settings", json={"provisioning_enabled": None}
    )
    assert cleared.json()["overrides"] == {}
    assert cleared.json()["effective"]["provisioning_enabled"] is True


@pytest.mark.parametrize(
    "body",
    [
        {"image": "evil:latest"},  # not overridable
        {"provisioning_enabled": "no"},
        {"idle_grace_seconds": "600"},
        {"idle_grace_seconds": True},
        {"idle_grace_seconds": -1},
        {"task_timeout_seconds": 0},
    ],
)
async def test_invalid_settings_are_rejected_and_nothing_is_written(admin, body):
    resp = await admin.op.put(
        "/admin/worker-settings", json={"max_vm_lifetime_hours": 5, **body}
    )

    assert resp.status_code == 422
    assert (await admin.op.get("/admin/worker-settings")).json()["overrides"] == {}


async def test_overrides_reach_a_running_service_on_refresh(db):
    await _wipe()
    live = WorkerDefaults()
    service = WorkerSettingsService(WorkerSettingsRepository(), live, refresh_seconds=0)

    await WorkerSettingsRepository().set("idle_grace_seconds", 5)
    await service.refresh()

    assert live.idle_grace_seconds == 5
    await WorkerSettingsRepository().unset("idle_grace_seconds")
    await service.refresh()
    assert live.idle_grace_seconds == WorkerDefaults().idle_grace_seconds


async def test_a_paused_system_refuses_new_workers_end_to_end(admin):
    await admin.op.put("/admin/worker-settings", json={"provisioning_enabled": False})
    admin.env.svc.settings.provisioning_enabled = (
        admin.settings.settings.provisioning_enabled
    )

    assert (await admin.op.post("/admin/workers")).status_code == 503


async def test_update_rejects_unknown_key_directly(db):
    service = WorkerSettingsService(WorkerSettingsRepository(), WorkerDefaults())

    with pytest.raises(InvalidSettingError):
        await service.update({"api_public_url": "https://evil"})


# -- reports ----------------------------------------------------------------------


def _reports(settings: WorkerDefaults | None = None) -> WorkerReportService:
    return WorkerReportService(
        WorkerRepository(),
        WorkerEventRepository(),
        WorkerTaskRepository(),
        settings or WorkerDefaults(hourly_cost_usd=0.5),
    )


async def test_cost_counts_vm_hours_and_prices_them(admin):
    worker_id = await admin.env.boot()
    # The VM has been up for two hours.
    await admin.env.repo.update_worker(
        worker_id,
        extra_patch={
            "booting_since": (datetime.now(UTC) - timedelta(hours=2)).isoformat()
        },
    )

    report = (await admin.op.get("/admin/cost?days=7")).json()

    assert report["vm_count"] == 1
    assert report["vm_hours"] == pytest.approx(2.0, abs=0.05)
    assert report["estimated_cost_usd"] == pytest.approx(1.0, abs=0.05)
    assert report["workers"][0]["vm_ended_at"] is None  # still running


async def test_cost_stops_counting_when_the_vm_is_deleted(admin):
    worker_id = await admin.env.boot()
    now = datetime.now(UTC)
    await admin.env.repo.update_worker(
        worker_id,
        state=WorkerState.DELETED,
        state_changed_at=now - timedelta(hours=1),
        extra_patch={"booting_since": (now - timedelta(hours=3)).isoformat()},
    )

    report = await _reports().cost(7)

    assert report["vm_hours"] == pytest.approx(2.0, abs=0.05)  # 3h ago .. 1h ago


async def test_cost_clips_to_the_window(admin):
    worker_id = await admin.env.boot()
    await admin.env.repo.update_worker(
        worker_id,
        extra_patch={
            "booting_since": (datetime.now(UTC) - timedelta(days=10)).isoformat()
        },
    )

    report = await _reports().cost(1)

    assert report["vm_hours"] == pytest.approx(24.0, abs=0.1)


async def test_cost_without_a_rate_reports_hours_only(admin):
    await admin.env.boot()

    report = await _reports(WorkerDefaults()).cost(7)

    assert report["estimated_cost_usd"] is None
    assert report["vm_hours"] >= 0


async def test_workers_that_never_got_a_vm_cost_nothing(admin):
    await admin.env.svc.request_worker(T)  # REQUESTED, no VM

    assert (await _reports().cost(7))["vm_count"] == 0


async def test_cold_start_is_request_to_ready(admin):
    events = WorkerEventRepository()
    await events.append("worker.requested", "r", worker_id="worker_c1")
    await asyncio.sleep(0.05)
    await events.append("worker.ready", "ok", worker_id="worker_c1")
    await events.append(
        "worker.requested", "r", worker_id="worker_c2"
    )  # never became ready

    stats = await _reports().cold_starts(7)

    assert stats["count"] == 1
    assert 0 < stats["avg_seconds"] < 5


async def test_overview_counts_workers_and_queue(admin):
    await admin.env.boot()
    await _pending_connection(admin)

    overview = (await admin.op.get("/admin/overview")).json()

    assert overview["workers"]["by_state"]["AVAILABLE/succeeded"] == 1
    assert overview["tasks"]["by_state"]["queued"] == 1
    assert overview["tasks"]["oldest_queued_age_seconds"] >= 0
