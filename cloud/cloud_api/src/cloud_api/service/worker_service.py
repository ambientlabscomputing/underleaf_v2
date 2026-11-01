"""
Worker service: owns the lifecycle of on-demand conn_worker VMs.

Each worker is a `(state, status)` pair: `state` is the node of the state
machine the worker is in (or heading to) and `status` says whether the
transition into it has been reached (in_progress / succeeded / failed).

    REQUESTED -> CREATED -> CONFIGURED -> AVAILABLE -> DRAINING -> DELETED
                  (VM)       (bootstrap     (healthy,     (idle)      (VM gone)
                              callback)      serving)

Every node is entered `IN_PROGRESS` and settles to `SUCCEEDED` (or `FAILED`):
`DRAINING + IN_PROGRESS` is the drain period, `DRAINING + SUCCEEDED` means
drained, `DELETED + IN_PROGRESS` is a teardown under way, and only
`DELETED + SUCCEEDED` means the worker is gone (and frees its slot).

`AVAILABLE + SUCCEEDED` is "ready to serve". Nothing here blocks on a VM:
`request_worker` records demand and returns; the reconciler (see
worker_reconciler.py) calls `reconcile()` on a tick and advances each worker
one step. Every write is a compare-and-set on `(state, status)`, so the
reconciler, the VM's callbacks and the admin CLI can all act without
clobbering each other.
"""

import asyncio
import hashlib
import hmac
import secrets
from collections.abc import Awaitable, Callable
from datetime import UTC, datetime, timedelta

import grpc
import grpc.aio

from cloud_api import logger
from cloud_api.config import WorkerDefaults
from cloud_api.lib.cert_lib import CertLib
from cloud_api.lib.conn_worker_client import ConnWorkerClient
from cloud_api.lib.dns import DNSManager
from cloud_api.lib.events import Event, EventBus
from cloud_api.lib.vm_manager import VM, VMManager, VMSpec
from cloud_api.models.api.common import Status
from cloud_api.models.api.worker import (
    QueryWorkerRequest,
    Worker,
    WorkerConfig,
    WorkerState,
    WorkerType,
)
from cloud_api.models.base import IDPrefix, generate_id
from cloud_api.repository.worker_event_repository import WorkerEventRepository
from cloud_api.repository.worker_repository import WorkerRepository
from cloud_api.service.worker_bootstrap import BootstrapParams, render_cloud_init

CONNECTION_WORKER_REPOSITORY = "ambientlabscomputing/underleafv2"
CONNECTION_WORKER_INSTALL_CMD = ["./install.sh"]
CONNECTION_WORKER_INIT_CMD = ["make", "run"]
CONNECTION_WORKER_CMD_CWD = "cloud/conn_worker"

WAKE_TOPIC = "workers.wake"  # something changed; reconcile now
READY_TOPIC = "workers.ready"  # a worker became AVAILABLE + SUCCEEDED

# Provider tags every worker VM carries; the orphan reaper keys off them.
TAG_APP = {"app": "underleaf", "role": "worker"}

API_IDENTITY = "cloud_api"  # CN the VM's nginx admits on the gRPC port


class WorkerUnavailableError(Exception):
    """No worker can be provided right now (disabled, misconfigured, circuit
    breaker open, or the live worker is going away)."""


class WorkerStateError(Exception):
    """The operation isn't valid for the worker's current state."""


class InvalidWorkerTokenError(Exception):
    """The bootstrap token is wrong, expired, or the worker isn't booting."""


def build_worker_config(settings: WorkerDefaults) -> WorkerConfig:
    return WorkerConfig(
        cpu=settings.cpu,
        mem_gb=settings.mem_gb,
        storage_gb=settings.storage_gb,
        repository=CONNECTION_WORKER_REPOSITORY,
        install_cmd=CONNECTION_WORKER_INSTALL_CMD,
        init_cmd=CONNECTION_WORKER_INIT_CMD,
        install_cmd_cwd=CONNECTION_WORKER_CMD_CWD,
        init_cmd_cwd=CONNECTION_WORKER_CMD_CWD,
        image=settings.image,
    )


def _iso(dt: datetime) -> str:
    return dt.isoformat()


def _parse(ts: str | None) -> datetime | None:
    return datetime.fromisoformat(ts) if ts else None


def _hash_token(token: str) -> str:
    return hashlib.sha256(token.encode()).hexdigest()


class WorkerService:
    def __init__(
        self,
        worker_repository: WorkerRepository,
        event_repository: WorkerEventRepository,
        vm_manager: VMManager,
        cert_lib: CertLib,
        settings: WorkerDefaults,
        event_bus: EventBus,
        has_pending_work: Callable[[], Awaitable[bool]] | None = None,
        dns_manager: DNSManager | None = None,
    ):
        self.worker_repository = worker_repository
        self.event_repository = event_repository
        self.vm_manager = vm_manager
        self.cert_lib = cert_lib
        self.settings = settings
        self.event_bus = event_bus
        # Queued work that should keep a worker alive even with no open
        # connections (the task queue plugs in here).
        self.has_pending_work = has_pending_work or _no_pending_work
        # Keeps the names agents dial pointed at the ready VM (None: by IP only).
        self.dns_manager = dns_manager
        self._clients: dict[str, ConnWorkerClient] = {}
        self._identity: tuple[str, str, datetime] | None = None  # cert, key, issued

    # ------------------------------------------------------------------
    # Demand
    # ------------------------------------------------------------------

    async def request_worker(self, worker_type: WorkerType) -> Worker:
        """Record demand for a worker; never blocks and is idempotent.

        Returns the type's live worker (cancelling a drain if it was idle),
        or creates one. The reconciler does the actual provisioning.
        """
        live = await self.worker_repository.get_live_worker(worker_type)
        if live is not None:
            if live.state == WorkerState.DRAINING:
                undrained = await self._cas(
                    live,
                    (live.state, live.status),
                    "worker.drain_cancelled",
                    "New demand arrived while draining",
                    state=WorkerState.AVAILABLE,
                    status=Status.SUCCEEDED,
                    extra_patch={"idle_since": None},
                )
                live = (
                    undrained
                    or await self.worker_repository.get_worker(live.id)
                    or live
                )
            await self._wake()
            return live

        await self._check_can_provision(worker_type)
        worker = Worker(
            id=generate_id(IDPrefix.WORKER),
            worker_type=worker_type,
            config=build_worker_config(self.settings),
            state=WorkerState.REQUESTED,
            status=Status.SUCCEEDED,  # "demand recorded" is reached immediately
        )
        worker, created = await self.worker_repository.get_or_create_live_worker(worker)
        if created:
            await self._emit(worker, "worker.requested", "Worker requested")
        await self._wake()
        return worker

    async def wait_ready(self, worker_id: str, timeout: float) -> Worker:
        """Wait for the worker to be ready. Events only wake us early; the DB
        is re-read at least every few seconds so a lost wakeup can't hang."""
        loop = asyncio.get_running_loop()
        deadline = loop.time() + timeout
        with self.event_bus.subscription(READY_TOPIC) as sub:
            while True:
                worker = await self.worker_repository.get_worker(worker_id)
                if worker is None or worker.state == WorkerState.DELETED:
                    raise WorkerUnavailableError(f"worker {worker_id} is gone")
                if worker.is_ready:
                    return worker
                remaining = deadline - loop.time()
                if remaining <= 0:
                    raise TimeoutError(f"worker {worker_id} not ready after {timeout}s")
                await sub.get(timeout=min(5.0, remaining))

    async def get_ready_worker(self, worker_type: WorkerType) -> Worker | None:
        live = await self.worker_repository.get_live_worker(worker_type)
        return live if live is not None and live.is_ready else None

    async def _check_can_provision(self, worker_type: WorkerType) -> None:
        s = self.settings
        if not s.provisioning_enabled:
            raise WorkerUnavailableError("worker provisioning is disabled")
        if not s.image:
            raise WorkerUnavailableError(
                "worker_defaults.image is not configured; nothing to run on the VM"
            )
        if not s.api_public_url:
            raise WorkerUnavailableError(
                "worker_defaults.api_public_url is not configured; the VM could not call back"
            )
        since = datetime.now(UTC) - timedelta(seconds=s.breaker_window_seconds)
        failed = await self.worker_repository.count_failed_since(worker_type, since)
        if failed >= s.breaker_threshold:
            raise WorkerUnavailableError(
                f"circuit breaker open: {failed} workers failed in the last "
                f"{s.breaker_window_seconds}s"
            )

    # ------------------------------------------------------------------
    # Reconciler
    # ------------------------------------------------------------------

    async def reconcile(self) -> None:
        """One tick: advance every live worker one step, then reap orphans."""
        for worker in await self.worker_repository.list_live_workers():
            with logger.contextualize(worker_id=worker.id):
                try:
                    await self.reconcile_worker(worker)
                except Exception as exc:  # noqa: BLE001 -- one bad worker mustn't stall the rest
                    logger.exception(f"reconcile failed: {exc}")
        try:
            await self.reap_orphans()
        except Exception as exc:  # noqa: BLE001
            logger.error(f"orphan reaping failed: {exc}")

    async def reconcile_worker(self, worker: Worker) -> None:
        key = (worker.state, worker.status)
        if worker.status == Status.FAILED:
            return await self._handle_failed(worker)
        match key:
            case (WorkerState.REQUESTED, _):
                await self._begin_create(worker)
            case (WorkerState.CREATED, Status.IN_PROGRESS):
                await self._create_vm(worker)  # crashed mid-create: idempotent re-run
            case (WorkerState.CREATED, Status.SUCCEEDED):
                await self._cas(
                    worker,
                    key,
                    "worker.awaiting_bootstrap",
                    "VM exists; waiting for it to bootstrap",
                    state=WorkerState.CONFIGURED,
                    status=Status.IN_PROGRESS,
                )
            case (WorkerState.CONFIGURED, Status.IN_PROGRESS):
                await self._await_bootstrap(worker)
            case (WorkerState.CONFIGURED, Status.SUCCEEDED):
                await self._cas(
                    worker,
                    key,
                    "worker.verifying",
                    "Bootstrap finished; verifying gRPC",
                    state=WorkerState.AVAILABLE,
                    status=Status.IN_PROGRESS,
                )
            case (WorkerState.AVAILABLE, Status.IN_PROGRESS):
                await self._verify_available(worker)
            case (WorkerState.AVAILABLE, Status.SUCCEEDED):
                await self._maintain(worker)
            case (WorkerState.DRAINING, Status.IN_PROGRESS):
                if _age(worker.state_changed_at) >= self.settings.drain_seconds:
                    await self._cas(
                        worker,
                        key,
                        "worker.drained",
                        "Drain period elapsed",
                        status=Status.SUCCEEDED,
                    )
            case (WorkerState.DRAINING, Status.SUCCEEDED):
                await self._cas(
                    worker,
                    key,
                    "worker.deleting",
                    "Drained; deleting VM",
                    state=WorkerState.DELETED,
                    status=Status.IN_PROGRESS,
                )
            case (WorkerState.DELETED, Status.IN_PROGRESS):
                await self._delete_vm(worker)

    # -- REQUESTED -> CREATED ------------------------------------------

    async def _begin_create(self, worker: Worker) -> None:
        if not self.settings.provisioning_enabled:
            return  # kill switch: leave it parked
        moved = await self._cas(
            worker,
            (worker.state, worker.status),
            "worker.creating",
            "Creating VM",
            state=WorkerState.CREATED,
            status=Status.IN_PROGRESS,
            extra_patch={"booting_since": _iso(datetime.now(UTC))},
        )
        if moved is not None:
            await self._create_vm(moved)

    async def _create_vm(self, worker: Worker) -> None:
        """Create the VM (or adopt one already tagged with this worker's id,
        so a crash between the provider call and our write can't leak a VM)."""
        expect = (WorkerState.CREATED, Status.IN_PROGRESS)
        try:
            existing = await self.vm_manager.list_vms(
                {**self._tags(), "worker_id": worker.id}
            )
            if existing:
                vm = existing[0]
            else:
                token = secrets.token_urlsafe(32)
                # Note: if we crash after create_vm but before the write below,
                # the adopted VM carries a token whose hash we no longer have;
                # it just times out and is retried, which is cheaper than
                # persisting plaintext tokens.
                await self.worker_repository.update_worker(
                    worker.id, extra_patch={"bootstrap_token_hash": _hash_token(token)}
                )
                vm = await self.vm_manager.create_vm(
                    f"underleaf-{worker.worker_type.value}-{worker.id}",
                    VMSpec(
                        cpus=worker.config.cpu,
                        mem_gb=worker.config.mem_gb,
                        storage_gb=worker.config.storage_gb,
                    ),
                    user_data=self._user_data(worker, token),
                    tags={**self._tags(), "worker_id": worker.id},
                )
        except Exception as exc:  # noqa: BLE001
            await self._fail(worker, expect, f"VM creation failed: {exc}")
            return
        await self._cas(
            worker,
            expect,
            "worker.created",
            f"VM {vm.provider_id} created",
            status=Status.SUCCEEDED,
            extra_patch={"vm": _vm_dump(vm)},
            data={"provider_id": vm.provider_id},
        )

    def _user_data(self, worker: Worker, token: str) -> str:
        s = self.settings
        return render_cloud_init(
            BootstrapParams(
                worker_id=worker.id,
                token=token,
                callback_url=f"{s.api_public_url.rstrip('/')}/internal/workers/{worker.id}",
                image=worker.config.image,
                ca_pem=self.cert_lib.ca_pem,
                gateway_domain=s.gateway_domain,
                grpc_tls_port=s.grpc_tls_port,
                api_egress_ip=s.api_egress_ip,
            )
        )

    def _tags(self) -> dict[str, str]:
        return {**TAG_APP, "env": self.settings.environment}

    # -- CONFIGURED: waiting on the VM ----------------------------------

    async def _await_bootstrap(self, worker: Worker) -> None:
        booting_since = (
            _parse(worker.extra_data.get("booting_since")) or worker.created_at
        )
        if (
            datetime.now(UTC) - booting_since
        ).total_seconds() > self.settings.boot_timeout_seconds:
            await self._fail(
                worker,
                (WorkerState.CONFIGURED, Status.IN_PROGRESS),
                f"VM did not become ready within {self.settings.boot_timeout_seconds}s",
            )
            return
        worker = await self._ensure_public_ip(worker)
        # Fallback for a lost "ready" callback: if the VM got its cert and
        # gRPC now answers over mTLS, it is up.
        if (
            worker.extra_data.get("registered")
            and worker.extra_data.get("vm", {}).get("public_ip")
            and await (await self.get_client(worker)).ping(timeout=3.0)
        ):
            await self._cas(
                worker,
                (WorkerState.CONFIGURED, Status.IN_PROGRESS),
                "worker.configured",
                "gRPC reachable (detected by health check)",
                status=Status.SUCCEEDED,
            )

    async def _ensure_public_ip(self, worker: Worker) -> Worker:
        vm = worker.extra_data.get("vm", {})
        if vm.get("public_ip") or not vm.get("provider_id"):
            return worker
        fresh = await self.vm_manager.get_vm(vm["provider_id"])
        if fresh is None or not fresh.public_ip:
            return worker
        updated = await self.worker_repository.update_worker(
            worker.id, extra_patch={"vm": _vm_dump(fresh)}
        )
        return updated or worker

    # -- AVAILABLE -------------------------------------------------------

    def _dns_names(self) -> list[str]:
        names = [self.settings.tunnel_hostname, *self.settings.dns_names]
        return list(dict.fromkeys(n for n in names if n))

    async def _publish_dns(self, worker: Worker) -> None:
        ip = worker.extra_data.get("vm", {}).get("public_ip")
        if self.dns_manager is None or not ip:
            return
        for name in self._dns_names():
            await self.dns_manager.upsert_a(name, ip)

    async def _unpublish_dns(self, worker: Worker) -> None:
        """Best effort: a stale record is overwritten by the next worker, and
        `ip=` keeps us from removing a record a newer worker already took."""
        ip = worker.extra_data.get("vm", {}).get("public_ip")
        if self.dns_manager is None or not ip:
            return
        for name in self._dns_names():
            try:
                await self.dns_manager.delete_a(name, ip=ip)
            except Exception:  # noqa: BLE001
                logger.exception(f"could not remove DNS record {name}")

    async def _verify_available(self, worker: Worker) -> None:
        expect = (WorkerState.AVAILABLE, Status.IN_PROGRESS)
        if await (await self.get_client(worker)).ping(timeout=5.0):
            try:
                await self._publish_dns(worker)
            except Exception as exc:  # noqa: BLE001 -- retried next tick
                await self._emit(
                    worker, "worker.dns_failed", f"DNS update failed: {exc}"
                )
                if (
                    _age(worker.state_changed_at)
                    > self.settings.unhealthy_grace_seconds
                ):
                    await self._fail(worker, expect, f"DNS update failed: {exc}")
                return
            ready = await self._cas(
                worker,
                expect,
                "worker.ready",
                "Worker is ready",
                status=Status.SUCCEEDED,
                extra_patch={"idle_since": None, "unhealthy_since": None},
            )
            if ready is not None:
                await self.event_bus.publish(
                    Event(READY_TOPIC, {"worker_id": worker.id})
                )
        elif _age(worker.state_changed_at) > self.settings.unhealthy_grace_seconds:
            await self._fail(worker, expect, "gRPC unreachable after bootstrap")

    async def _maintain(self, worker: Worker) -> None:
        """Health, idle and lifetime checks for a ready worker."""
        s = self.settings
        now = datetime.now(UTC)
        expect = (WorkerState.AVAILABLE, Status.SUCCEEDED)

        vm_created = _parse(worker.extra_data.get("booting_since")) or worker.created_at
        if s.max_vm_lifetime_hours and now - vm_created > timedelta(
            hours=s.max_vm_lifetime_hours
        ):
            await self._drain(worker, f"VM older than {s.max_vm_lifetime_hours}h")
            return

        client = await self.get_client(worker)
        if not await client.ping(timeout=3.0):
            since = _parse(worker.extra_data.get("unhealthy_since"))
            if since is None:
                await self.worker_repository.update_worker(
                    worker.id, extra_patch={"unhealthy_since": _iso(now)}
                )
            elif (now - since).total_seconds() > s.unhealthy_grace_seconds:
                await self._fail(worker, expect, "worker unreachable; replacing it")
            return
        if worker.extra_data.get("unhealthy_since"):
            await self.worker_repository.update_worker(
                worker.id, extra_patch={"unhealthy_since": None}
            )

        try:
            busy = (
                bool(await client.list_connections()) or await self.has_pending_work()
            )
        except grpc.aio.AioRpcError as exc:
            logger.warning(f"busy probe failed: {exc.code()}")
            return  # unknown; don't let a flaky probe start an idle clock
        if busy:
            if worker.extra_data.get("idle_since"):
                await self.worker_repository.update_worker(
                    worker.id, extra_patch={"idle_since": None}
                )
            return
        idle_since = _parse(worker.extra_data.get("idle_since"))
        if idle_since is None:
            await self.worker_repository.update_worker(
                worker.id, extra_patch={"idle_since": _iso(now)}
            )
        elif (now - idle_since).total_seconds() >= s.idle_grace_seconds:
            await self._drain(worker, f"idle for {s.idle_grace_seconds}s")

    async def _drain(self, worker: Worker, reason: str) -> None:
        await self._cas(
            worker,
            (WorkerState.AVAILABLE, Status.SUCCEEDED),
            "worker.draining",
            f"Draining: {reason}",
            state=WorkerState.DRAINING,
            status=Status.IN_PROGRESS,
            extra_patch={"idle_since": None},
        )

    # -- failure / teardown ----------------------------------------------

    async def _fail(
        self, worker: Worker, expect: tuple[WorkerState, Status], error: str
    ) -> None:
        failures = int(worker.extra_data.get("failures", 0)) + 1
        logger.bind(state=worker.state).warning(error)
        await self._cas(
            worker,
            expect,
            "worker.failed",
            error,
            status=Status.FAILED,
            error=error,
            extra_patch={"failures": failures},
            data={"failures": failures},
        )

    async def _handle_failed(self, worker: Worker) -> None:
        """FAILED steps before the worker ever served are retried on a fresh
        VM with backoff; anything else, or too many failures, is torn down."""
        failures = int(worker.extra_data.get("failures", 0))
        retryable = (
            worker.state in (WorkerState.CREATED, WorkerState.CONFIGURED)
            and failures < self.settings.max_provision_attempts
        )
        if retryable:
            backoff = min(300, 5 * 2**failures)
            if _age(worker.state_changed_at) < backoff:
                return
            await self._delete_vm_quietly(worker)
            await self._cas(
                worker,
                (worker.state, Status.FAILED),
                "worker.retrying",
                f"Retrying provisioning (attempt {failures + 1})",
                state=WorkerState.REQUESTED,
                status=Status.SUCCEEDED,
                error=None,  # this attempt is over; only a final failure counts
                extra_patch={"vm": None, "registered": None, "booting_since": None},
            )
            return
        await self._cas(
            worker,
            (worker.state, Status.FAILED),
            "worker.deleting",
            "Giving up; deleting VM",
            state=WorkerState.DELETED,
            status=Status.IN_PROGRESS,
        )

    async def _delete_vm(self, worker: Worker) -> None:
        expect = (WorkerState.DELETED, Status.IN_PROGRESS)
        try:
            await self._delete_vm_quietly(worker, raise_errors=True)
        except Exception as exc:  # noqa: BLE001 -- stays in progress; retried next tick
            await self._emit(worker, "worker.delete_failed", f"VM delete failed: {exc}")
            return
        await self._cas(
            worker,
            expect,
            "worker.deleted",
            "Worker deleted",
            # Reached. Any `error` recorded earlier stays on the row: that is
            # what marks this worker as a failed one for the circuit breaker.
            status=Status.SUCCEEDED,
        )

    async def _delete_vm_quietly(
        self, worker: Worker, raise_errors: bool = False
    ) -> None:
        client = self._clients.pop(worker.id, None)
        if client is not None:
            await client.close()
        try:
            provider_ids = {worker.extra_data.get("vm", {}).get("provider_id")}
            for vm in await self.vm_manager.list_vms(
                {**self._tags(), "worker_id": worker.id}
            ):
                provider_ids.add(vm.provider_id)
            for provider_id in filter(None, provider_ids):
                await self.vm_manager.delete_vm(provider_id)
            await self._unpublish_dns(worker)
        except Exception:
            if raise_errors:
                raise
            logger.exception("VM delete failed; the orphan reaper will retry")

    async def destroy_worker(
        self, worker_id: str, reason: str = "destroyed by admin"
    ) -> Worker:
        """Force teardown from any live state (admin)."""
        for _ in range(3):  # lose a CAS race to the reconciler -> re-read, retry
            worker = await self.worker_repository.get_worker(worker_id)
            if worker is None:
                raise KeyError(worker_id)
            if worker.state == WorkerState.DELETED:  # already tearing down / gone
                return worker
            moved = await self._cas(
                worker,
                (worker.state, worker.status),
                "worker.deleting",
                reason,
                state=WorkerState.DELETED,
                status=Status.IN_PROGRESS,
            )
            if moved is not None:
                await self._wake()
                return moved
        raise RuntimeError(f"could not start deleting worker {worker_id}")

    async def reap_orphans(self) -> list[VM]:
        """Delete tagged VMs that no live worker owns (crash leftovers, or a
        worker row that was removed). Skips young VMs so an in-flight create
        isn't mistaken for an orphan."""
        live = {w.id: w for w in await self.worker_repository.list_live_workers()}
        min_age = self.settings.orphan_min_age_seconds
        reaped = []
        for vm in await self.vm_manager.list_vms(self._tags()):
            worker = live.get(vm.tags.get("worker_id", ""))
            if (
                worker is not None
                and worker.extra_data.get("vm", {}).get("provider_id", vm.provider_id)
                == vm.provider_id
            ):
                continue
            if vm.created_at is not None and _age(vm.created_at) < min_age:
                continue
            await self.vm_manager.delete_vm(vm.provider_id)
            reaped.append(vm)
            await self.event_repository.append(
                "vm.orphan_reaped",
                f"Deleted orphan VM {vm.provider_id}",
                worker_id=vm.tags.get("worker_id"),
                data={"provider_id": vm.provider_id},
            )
            logger.bind(provider_id=vm.provider_id).warning("Reaped orphan VM")
        return reaped

    async def audit_vms(self) -> dict:
        """Provider truth against our records: every VM we manage with the
        worker that owns it (None = an orphan the reaper will take once it is
        old enough), plus live workers whose VM the provider no longer has."""
        live = {w.id: w for w in await self.worker_repository.list_live_workers()}
        vms = await self.vm_manager.list_vms(self._tags())
        seen: set[str] = set()
        rows = []
        for vm in vms:
            worker = live.get(vm.tags.get("worker_id", ""))
            owned = (
                worker is not None
                and worker.extra_data.get("vm", {}).get("provider_id", vm.provider_id)
                == vm.provider_id
            )
            if owned:
                seen.add(worker.id)
            rows.append(
                {
                    "vm": _vm_dump(vm),
                    "worker_id": worker.id if owned else None,
                    "worker_state": worker.state.value if owned else None,
                    "orphan": not owned,
                }
            )
        missing = [
            {
                "worker_id": w.id,
                "provider_id": w.extra_data.get("vm", {}).get("provider_id"),
            }
            for w in live.values()
            if w.extra_data.get("vm", {}).get("provider_id") and w.id not in seen
        ]
        return {"vms": rows, "missing": missing}

    async def delete_managed_vm(self, provider_id: str) -> VM:
        """Delete one VM by hand, but only one this deployment manages: an
        operator typo must not be able to destroy an unrelated droplet."""
        for vm in await self.vm_manager.list_vms(self._tags()):
            if vm.provider_id == provider_id:
                await self.vm_manager.delete_vm(provider_id)
                await self.event_repository.append(
                    "vm.deleted_by_admin",
                    f"Deleted VM {provider_id}",
                    worker_id=vm.tags.get("worker_id"),
                    data={"provider_id": provider_id},
                )
                return vm
        raise KeyError(provider_id)

    async def drain_worker(
        self, worker_id: str, reason: str = "drained by admin"
    ) -> Worker:
        """Take a ready worker out of service; the reconciler deletes it once
        the drain period is over. New demand cancels the drain."""
        worker = await self.worker_repository.get_worker(worker_id)
        if worker is None:
            raise KeyError(worker_id)
        if not worker.is_ready:
            raise WorkerStateError(
                f"worker {worker_id} is {worker.state.value}/{worker.status.value}; "
                "only a ready worker can be drained"
            )
        await self._drain(worker, reason)
        return await self.worker_repository.get_worker(worker_id) or worker

    # ------------------------------------------------------------------
    # Callbacks from the VM
    # ------------------------------------------------------------------

    async def register_worker(
        self, worker_id: str, token: str, csr_pem: str
    ) -> tuple[str, str]:
        """The VM sends a CSR; we return a server cert for its public IP.
        The private key never leaves the VM."""
        worker = await self._authenticate_vm(worker_id, token)
        worker = await self._ensure_public_ip(worker)
        public_ip = worker.extra_data.get("vm", {}).get("public_ip")
        if not public_ip:
            raise InvalidWorkerTokenError("VM has no public IP yet")
        dns = [self.settings.tunnel_hostname] if self.settings.tunnel_hostname else []
        try:
            cert, ca = self.cert_lib.issue_server_cert(
                csr_pem, common_name=worker.id, dns_names=dns, ip_addresses=[public_ip]
            )
        except ValueError as exc:
            raise InvalidWorkerTokenError(f"bad CSR: {exc}") from exc
        await self.worker_repository.update_worker(
            worker.id, extra_patch={"registered": True}
        )
        await self._emit(worker, "worker.registered", "Issued server certificate")
        return cert, ca

    async def mark_ready(self, worker_id: str, token: str) -> Worker:
        """The VM says conn_worker is up. Idempotent."""
        worker = await self._authenticate_vm(worker_id, token)
        for _ in range(3):
            if worker.state in (WorkerState.AVAILABLE, WorkerState.DRAINING) or (
                worker.state == WorkerState.CONFIGURED
                and worker.status == Status.SUCCEEDED
            ):
                return worker
            moved = await self._cas(
                worker,
                (worker.state, worker.status),
                "worker.configured",
                "VM reported ready",
                state=WorkerState.CONFIGURED,
                status=Status.SUCCEEDED,
            )
            if moved is not None:
                await self._wake()
                return moved
            worker = await self._authenticate_vm(worker_id, token)
        return worker

    async def _authenticate_vm(self, worker_id: str, token: str) -> Worker:
        worker = await self.worker_repository.get_worker(worker_id)
        expected = worker.extra_data.get("bootstrap_token_hash") if worker else None
        if (
            worker is None
            or not expected
            or not hmac.compare_digest(expected, _hash_token(token))
            or worker.state
            not in (WorkerState.CREATED, WorkerState.CONFIGURED, WorkerState.AVAILABLE)
            or worker.status == Status.FAILED
        ):
            raise InvalidWorkerTokenError("invalid bootstrap token")
        return worker

    # ------------------------------------------------------------------
    # API -> worker gRPC (mTLS)
    # ------------------------------------------------------------------

    async def get_client(self, worker: Worker) -> ConnWorkerClient:
        ip = worker.extra_data.get("vm", {}).get("public_ip")
        if not ip:
            raise WorkerUnavailableError(f"worker {worker.id} has no public IP")
        target = f"{ip}:{self.settings.grpc_tls_port}"
        client = self._clients.get(worker.id)
        if client is None or client.target != target:
            if client is not None:
                await client.close()
            cert, key = self._client_identity()
            client = ConnWorkerClient(
                target,
                credentials=grpc.ssl_channel_credentials(
                    root_certificates=self.cert_lib.ca_pem.encode(),
                    private_key=key.encode(),
                    certificate_chain=cert.encode(),
                ),
            )
            self._clients[worker.id] = client
        return client

    def _client_identity(self) -> tuple[str, str]:
        now = datetime.now(UTC)
        if self._identity is None or now - self._identity[2] > timedelta(days=300):
            cert, key = self.cert_lib.issue_client_identity(
                API_IDENTITY, validity_days=365
            )
            self._identity = (cert, key, now)
        return self._identity[0], self._identity[1]

    async def close(self) -> None:
        for client in self._clients.values():
            await client.close()
        self._clients.clear()

    # ------------------------------------------------------------------
    # Queries (admin / CLI)
    # ------------------------------------------------------------------

    async def list_workers(self, query: QueryWorkerRequest) -> tuple[int, list[Worker]]:
        return await self.worker_repository.list_workers(query)

    async def get_worker(self, worker_id: str) -> Worker | None:
        return await self.worker_repository.get_worker(worker_id)

    # ------------------------------------------------------------------
    # helpers
    # ------------------------------------------------------------------

    async def _cas(
        self,
        worker: Worker,
        expect: tuple[WorkerState, Status],
        event_type: str,
        message: str,
        *,
        data: dict | None = None,
        **values,
    ) -> Worker | None:
        """Compare-and-set the worker, then record the transition. None means
        another actor moved it first (the caller just gives up this tick)."""
        moved = await self.worker_repository.transition(worker.id, expect, **values)
        if moved is None:
            return None
        await self._emit(moved, event_type, message, data)
        return moved

    async def _emit(
        self, worker: Worker, type: str, message: str, data: dict | None = None
    ) -> None:
        logger.bind(worker_id=worker.id, state=worker.state, status=worker.status).info(
            message
        )
        await self.event_repository.append(
            type,
            message,
            worker_id=worker.id,
            data={
                "state": worker.state.value,
                "status": worker.status.value,
                **(data or {}),
            },
        )

    async def _wake(self) -> None:
        await self.event_bus.publish(Event(WAKE_TOPIC))


async def _no_pending_work() -> bool:
    return False


def _age(ts: datetime | None) -> float:
    if ts is None:
        return float("inf")
    return (datetime.now(UTC) - ts).total_seconds()


def _vm_dump(vm: VM) -> dict:
    return vm.model_dump(mode="json")
