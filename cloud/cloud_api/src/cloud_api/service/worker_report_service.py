"""Read-only operator reports: what the workers cost, how fast they start, and
what is queued. Everything is derived from `workers`, `worker_events` and
`worker_tasks`, so it is as trustworthy as the timeline itself."""

import statistics
from datetime import UTC, datetime, timedelta

from cloud_api.config import WorkerDefaults
from cloud_api.models.api.worker import Worker
from cloud_api.repository.worker_event_repository import WorkerEventRepository
from cloud_api.repository.worker_repository import WorkerRepository
from cloud_api.repository.worker_task_repository import WorkerTaskRepository


def _parse(ts: str | None) -> datetime | None:
    return datetime.fromisoformat(ts) if ts else None


def _vm_window(worker: Worker, now: datetime) -> tuple[datetime, datetime] | None:
    """When the worker's VM existed (and so was billed); None if it never got one."""
    if not worker.extra_data.get("vm", {}).get("provider_id"):
        return None
    start = _parse(worker.extra_data.get("booting_since")) or worker.created_at
    # Billed until the VM is actually gone; a teardown in progress still bills.
    end = worker.state_changed_at if worker.is_gone and worker.state_changed_at else now
    return start, max(end, start)


class WorkerReportService:
    def __init__(
        self,
        worker_repo: WorkerRepository,
        event_repo: WorkerEventRepository,
        task_repo: WorkerTaskRepository,
        settings: WorkerDefaults,
    ):
        self.worker_repo = worker_repo
        self.event_repo = event_repo
        self.task_repo = task_repo
        self.settings = settings

    async def cost(self, days: int = 30) -> dict:
        """VM-hours over the last `days` and an estimate of their price.

        An estimate: billed time is taken from when we asked for the VM to when
        the delete succeeded, priced at `worker_defaults.hourly_cost_usd`. Check
        it against the provider's invoice before trusting it for accounting.
        """
        now = datetime.now(UTC)
        since = now - timedelta(days=days)
        rows = []
        total_seconds = 0.0
        for worker in await self.worker_repo.list_active_since(since):
            window = _vm_window(worker, now)
            if window is None:
                continue
            start, end = max(window[0], since), window[1]
            seconds = max((end - start).total_seconds(), 0.0)
            if seconds == 0:
                continue
            total_seconds += seconds
            rows.append(
                {
                    "worker_id": worker.id,
                    "state": worker.state.value,
                    "failed": bool(worker.error),
                    "vm_started_at": window[0].isoformat(),
                    "vm_ended_at": None
                    if not worker.is_gone
                    else window[1].isoformat(),
                    "billed_seconds": round(seconds),
                }
            )
        hours = total_seconds / 3600
        rate = self.settings.hourly_cost_usd
        return {
            "days": days,
            "since": since.isoformat(),
            "vm_count": len(rows),
            "vm_hours": round(hours, 3),
            "hourly_cost_usd": rate or None,
            "estimated_cost_usd": round(hours * rate, 4) if rate else None,
            "workers": rows,
        }

    async def cold_starts(self, days: int = 30) -> dict:
        """Request -> ready durations: the number behind "why did my connection
        take 4 minutes?"."""
        since = datetime.now(UTC) - timedelta(days=days)
        events = await self.event_repo.list_events(
            types=["worker.requested", "worker.ready"], since=since, limit=5000
        )
        requested: dict[str, datetime] = {}
        durations: list[float] = []
        for e in events:  # oldest first
            if e.worker_id is None:
                continue
            if e.type == "worker.requested":
                requested[e.worker_id] = e.created_at
            elif e.worker_id in requested:
                durations.append(
                    (e.created_at - requested.pop(e.worker_id)).total_seconds()
                )
        return {
            "days": days,
            "count": len(durations),
            "avg_seconds": round(statistics.fmean(durations), 1) if durations else None,
            "median_seconds": round(statistics.median(durations), 1)
            if durations
            else None,
            "max_seconds": round(max(durations), 1) if durations else None,
        }

    async def overview(self) -> dict:
        live = await self.worker_repo.list_live_workers()
        by_state: dict[str, int] = {}
        for w in live:
            key = f"{w.state.value}/{w.status.value}"
            by_state[key] = by_state.get(key, 0) + 1
        return {
            "workers": {"live": len(live), "by_state": by_state},
            "tasks": await self.task_repo.stats(),
            "cold_starts_30d": await self.cold_starts(30),
        }
