"""Callbacks a worker VM makes during bootstrap.

Not user-facing: authenticated by the one-time bootstrap token baked into the
VM's cloud-init, not by a user/node identity. Unknown worker, wrong token and
wrong state all look the same (401) so worker ids can't be probed.

  POST /internal/workers/{id}/register  CSR -> server certificate
  POST /internal/workers/{id}/ready     conn_worker is up
"""

from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel

from cloud_api.service.worker_service import InvalidWorkerTokenError, WorkerService
from cloud_api.service_manager import get_worker_service

router = APIRouter(prefix="/internal/workers", tags=["Internal"])


class RegisterWorkerRequest(BaseModel):
    token: str
    csr: str


class RegisterWorkerResponse(BaseModel):
    certificate: str
    ca: str


class WorkerReadyRequest(BaseModel):
    token: str


def _unauthorized() -> HTTPException:
    return HTTPException(
        status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid bootstrap token"
    )


@router.post("/{worker_id}/register", response_model=RegisterWorkerResponse)
async def register_worker(
    worker_id: str,
    req: RegisterWorkerRequest,
    svc: WorkerService = Depends(get_worker_service),
) -> RegisterWorkerResponse:
    try:
        certificate, ca = await svc.register_worker(worker_id, req.token, req.csr)
    except InvalidWorkerTokenError:
        raise _unauthorized()
    return RegisterWorkerResponse(certificate=certificate, ca=ca)


@router.post("/{worker_id}/ready", status_code=status.HTTP_204_NO_CONTENT)
async def worker_ready(
    worker_id: str,
    req: WorkerReadyRequest,
    svc: WorkerService = Depends(get_worker_service),
) -> None:
    try:
        await svc.mark_ready(worker_id, req.token)
    except InvalidWorkerTokenError:
        raise _unauthorized()
