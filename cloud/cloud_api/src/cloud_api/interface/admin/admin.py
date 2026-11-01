"""Operator API for the on-demand worker system. Every route requires an
account listed in `admin.principal_account_ids` (anyone else gets 404)."""

from typing import Annotated, Any

from fastapi import APIRouter, Depends, HTTPException, Query, status
from pydantic import BaseModel

from cloud_api.interface.deps import require_admin
from cloud_api.models.api import ConnectionState
from cloud_api.models.api.common import Status
from cloud_api.models.api.worker import (
    QueryWorkerRequest,
    Worker,
    WorkerEvent,
    WorkerState,
    WorkerTask,
    WorkerTaskState,
    WorkerType,
)
from cloud_api.service.connection_service import ConnectionService
from cloud_api.service.worker_report_service import WorkerReportService
from cloud_api.service.worker_service import (
    WorkerService,
    WorkerStateError,
    WorkerUnavailableError,
)
from cloud_api.service.worker_settings_service import (
    InvalidSettingError,
    WorkerSettingsService,
)
from cloud_api.service_manager import (
    get_connection_service,
    get_worker_event_repo,
    get_worker_report_service,
    get_worker_service,
    get_worker_settings_service,
    get_worker_task_repo,
)

router = APIRouter(
    prefix="/admin",
    tags=["Admin"],
    dependencies=[Depends(require_admin)],
)


def _not_found(what: str) -> HTTPException:
    return HTTPException(status.HTTP_404_NOT_FOUND, detail=f"{what} not found")


class WorkerDetail(BaseModel):
    worker: Worker
    events: list[WorkerEvent]


class WorkerList(BaseModel):
    items: list[Worker]
    total: int


# -- overview / cost ----------------------------------------------------------


@router.get("/overview", summary="Workers, queue depth and cold-start times")
async def overview(
    reports: WorkerReportService = Depends(get_worker_report_service),
) -> dict[str, Any]:
    return await reports.overview()


@router.get("/cost", summary="Estimated VM hours and spend")
async def cost(
    days: Annotated[int, Query(ge=1, le=365)] = 30,
    reports: WorkerReportService = Depends(get_worker_report_service),
) -> dict[str, Any]:
    return await reports.cost(days)


# -- workers --------------------------------------------------------------------


@router.get("/workers", response_model=WorkerList, summary="List workers")
async def list_workers(
    state: WorkerState | None = None,
    worker_status: Annotated[Status | None, Query(alias="status")] = None,
    limit: Annotated[int, Query(ge=1, le=200)] = 50,
    offset: Annotated[int, Query(ge=0)] = 0,
    service: WorkerService = Depends(get_worker_service),
) -> WorkerList:
    total, items = await service.list_workers(
        QueryWorkerRequest(
            state=state,
            status=worker_status,
            limit=limit,
            offset=offset,
            order_by="created_at",
            order_direction="desc",
        )
    )
    return WorkerList(items=items, total=total)


@router.post(
    "/workers",
    response_model=Worker,
    status_code=status.HTTP_202_ACCEPTED,
    summary="Pre-warm: request a worker now",
)
async def prewarm(service: WorkerService = Depends(get_worker_service)) -> Worker:
    try:
        return await service.request_worker(WorkerType.CONNECTION_WORKER)
    except WorkerUnavailableError as exc:
        raise HTTPException(status.HTTP_503_SERVICE_UNAVAILABLE, detail=str(exc))


@router.get(
    "/workers/{worker_id}",
    response_model=WorkerDetail,
    summary="A worker and its timeline",
)
async def get_worker(
    worker_id: str,
    service: WorkerService = Depends(get_worker_service),
) -> WorkerDetail:
    worker = await service.get_worker(worker_id)
    if worker is None:
        raise _not_found("Worker")
    events = await get_worker_event_repo().list_events(worker_id=worker_id)
    return WorkerDetail(worker=worker, events=events)


@router.post(
    "/workers/{worker_id}/drain",
    response_model=Worker,
    summary="Take a ready worker out of service",
)
async def drain_worker(
    worker_id: str, service: WorkerService = Depends(get_worker_service)
) -> Worker:
    try:
        return await service.drain_worker(worker_id)
    except KeyError:
        raise _not_found("Worker")
    except WorkerStateError as exc:
        raise HTTPException(status.HTTP_409_CONFLICT, detail=str(exc))


@router.post(
    "/workers/{worker_id}/destroy",
    response_model=Worker,
    summary="Force teardown from any state",
)
async def destroy_worker(
    worker_id: str, service: WorkerService = Depends(get_worker_service)
) -> Worker:
    try:
        return await service.destroy_worker(worker_id)
    except KeyError:
        raise _not_found("Worker")


# -- tasks & connections -------------------------------------------------------


@router.get("/worker-tasks", response_model=list[WorkerTask], summary="The queue")
async def list_tasks(
    state: WorkerTaskState | None = None,
    limit: Annotated[int, Query(ge=1, le=500)] = 100,
) -> list[WorkerTask]:
    return await get_worker_task_repo().list_tasks(state=state, limit=limit)


@router.post(
    "/worker-tasks/{task_id}/cancel",
    response_model=WorkerTask,
    summary="Cancel a queued task (and terminate its pending connection)",
)
async def cancel_task(
    task_id: str,
    connections: ConnectionService = Depends(get_connection_service),
) -> WorkerTask:
    tasks = get_worker_task_repo()
    task = await tasks.get(task_id)
    if task is None:
        raise _not_found("Task")
    if task.state not in (WorkerTaskState.QUEUED, WorkerTaskState.RUNNING):
        raise HTTPException(
            status.HTTP_409_CONFLICT, detail=f"task is already {task.state.value}"
        )
    conn = (
        await connections.conn_repo.get_connection(task.connection_id)
        if task.connection_id
        else None
    )
    if conn is not None and conn.state == ConnectionState.PENDING:
        # Goes through the connection so its slot is released exactly once.
        await connections.terminate_connection(conn.id)
    await tasks.cancel(task_id, "cancelled by admin")
    return await tasks.get(task_id) or task


@router.get(
    "/connections/{connection_id}/events",
    response_model=list[WorkerEvent],
    summary="Why did this connection take that long?",
)
async def connection_timeline(connection_id: str) -> list[WorkerEvent]:
    return await get_worker_event_repo().list_events(connection_id=connection_id)


# -- VMs (provider truth) -------------------------------------------------------


@router.get("/vms", summary="Provider VMs vs our records (orphans, missing)")
async def list_vms(
    service: WorkerService = Depends(get_worker_service),
) -> dict[str, Any]:
    return await service.audit_vms()


@router.post("/vms/{provider_id}/delete", summary="Delete a VM this deployment manages")
async def delete_vm(
    provider_id: str, service: WorkerService = Depends(get_worker_service)
) -> dict[str, str]:
    try:
        await service.delete_managed_vm(provider_id)
    except KeyError:
        raise _not_found("VM")
    return {"deleted": provider_id}


# -- settings --------------------------------------------------------------------


@router.get("/worker-settings", summary="Effective settings and active overrides")
async def get_settings(
    service: WorkerSettingsService = Depends(get_worker_settings_service),
) -> dict[str, Any]:
    return await service.describe()


@router.put(
    "/worker-settings",
    summary="Override settings; null clears an override (e.g. pause provisioning)",
)
async def put_settings(
    changes: dict[str, Any],
    service: WorkerSettingsService = Depends(get_worker_settings_service),
) -> dict[str, Any]:
    try:
        return await service.update(changes)
    except InvalidSettingError as exc:
        raise HTTPException(status.HTTP_422_UNPROCESSABLE_ENTITY, detail=str(exc))
