"""Operate on-demand conn_worker VMs: `python -m cloud_api.scripts.workerctl <cmd>`

  status               live workers and their (state, status)
  events <worker_id>   a worker's timeline
  prewarm              request a worker now (the API's reconciler provisions it)
  destroy <worker_id>  force teardown (the API's reconciler deletes the VM)
  reap-orphans         delete tagged VMs that no live worker owns
  tasks                the connection queue (--state to filter)
  cost [--days N]      VM hours and estimated spend
  settings             effective settings and active overrides
  pause / resume       provisioning kill switch (persisted; the API picks it
                       up within seconds). Existing workers keep running.

Reads the same config as the API (UNDERLEAF_CONFIG, UNDERLEAF_DO_TOKEN) and
talks to the same database, so it works even if the HTTP layer is down.
`prewarm` and `destroy` only record intent -- the running API's reconciler
does the work, which keeps a single writer driving each worker.
"""

import argparse
import asyncio
import json

import cloud_api
from cloud_api import load_config
from cloud_api.models.api.worker import WorkerTaskState, WorkerType
from cloud_api.repository import session as db_session


async def _main(args: argparse.Namespace) -> int:
    cfg = load_config()
    cloud_api.app_config = cfg
    db_session.init_session_maker(cfg)
    from cloud_api.service_manager import (
        get_worker_event_repo,
        get_worker_report_service,
        get_worker_service,
        get_worker_settings_service,
        get_worker_task_repo,
    )

    svc = get_worker_service()
    try:
        match args.cmd:
            case "status":
                workers = await svc.worker_repository.list_live_workers()
                if not workers:
                    print("no live workers")
                for w in workers:
                    vm = w.extra_data.get("vm", {})
                    print(
                        f"{w.id}  {w.state.value:<10} {w.status.value:<11} "
                        f"ip={vm.get('public_ip') or '-':<15} "
                        f"changed={w.state_changed_at:%H:%M:%S} "
                        f"{('error=' + w.error) if w.error else ''}"
                    )
            case "events":
                for e in await get_worker_event_repo().list_events(
                    worker_id=args.worker_id
                ):
                    print(f"{e.created_at:%H:%M:%S}  {e.type:<22} {e.message}")
            case "prewarm":
                w = await svc.request_worker(WorkerType.CONNECTION_WORKER)
                print(f"{w.id}  {w.state.value}/{w.status.value}")
            case "destroy":
                w = await svc.destroy_worker(args.worker_id)
                print(f"{w.id}  {w.state.value}/{w.status.value}")
            case "tasks":
                state = WorkerTaskState(args.state) if args.state else None
                tasks = await get_worker_task_repo().list_tasks(state=state, limit=50)
                if not tasks:
                    print("no tasks")
                for t in tasks:
                    print(
                        f"{t.id}  {t.state.value:<9} attempts={t.attempts}/{t.max_attempts} "
                        f"conn={t.connection_id or '-'} "
                        f"{('error=' + t.error) if t.error else ''}"
                    )
            case "cost":
                report = await get_worker_report_service().cost(args.days)
                est = report["estimated_cost_usd"]
                print(
                    f"last {args.days}d: {report['vm_count']} VM(s), "
                    f"{report['vm_hours']} VM-hours, "
                    + (
                        f"~${est}"
                        if est is not None
                        else "cost unknown (set worker_defaults.hourly_cost_usd)"
                    )
                )
            case "settings":
                print(
                    json.dumps(await get_worker_settings_service().describe(), indent=2)
                )
            case "pause" | "resume":
                await get_worker_settings_service().update(
                    {"provisioning_enabled": False if args.cmd == "pause" else None}
                )
                print(
                    "provisioning paused"
                    if args.cmd == "pause"
                    else "provisioning back to the config default"
                )
            case "reap-orphans":
                reaped = await svc.reap_orphans()
                print(f"reaped {len(reaped)} orphan VM(s)")
                for vm in reaped:
                    print(f"  {vm.provider_id}  {vm.hostname}")
    finally:
        await svc.close()
        await db_session.get_engine().dispose()
    return 0


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="cmd", required=True)
    sub.add_parser("status")
    sub.add_parser("prewarm")
    sub.add_parser("reap-orphans")
    sub.add_parser("settings")
    sub.add_parser("pause")
    sub.add_parser("resume")
    sub.add_parser("tasks").add_argument(
        "--state", choices=[s.value for s in WorkerTaskState]
    )
    sub.add_parser("cost").add_argument("--days", type=int, default=30)
    for name in ("events", "destroy"):
        sub.add_parser(name).add_argument("worker_id")
    raise SystemExit(asyncio.run(_main(parser.parse_args())))


if __name__ == "__main__":
    main()
