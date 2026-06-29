from functools import lru_cache

from cloud_api.repository.user_repository import UserRepository
from fastapi import APIRouter, FastAPI, Request, Depends, HTTPException, status
from fastapi.middleware.cors import CORSMiddleware
from cloud_api import app_config
from cloud_api.interface.accounts.accounts import router as accounts_router
from cloud_api.interface.billing_accounts.billing_accounts import (
    router as billing_accounts_router,
)
from cloud_api.interface.clusters.clusters import router as clusters_router
from cloud_api.interface.streams.streams import router as stream_router
from cloud_api.interface.oauth.oauth import router as oauth_router
from cloud_api.interface.registration.registration import router as registration_router
from cloud_api.interface.subscriptions.subscriptions import (
    router as subscriptions_router,
)
from cloud_api.interface.connections.connections import router as connections_router
from cloud_api.service_manager import get_node_repo, get_cluster_repo, get_user_repo
from cloud_api.lib.auth_lib import AuthLib
from scipy import cluster
from cloud_api.interface.deps import get_auth_lib, mint_token, fetch_data_for_node_or_cluster, x_subject_id_token_middleware_

app = FastAPI()
if not app_config:
    raise RuntimeError("Failed to load app config")

app.add_middleware(
    CORSMiddleware,
    allow_origins=app_config.cors.allowed_origins,
    allow_credentials=True,
    allow_methods=app_config.cors.allowed_methods,
    allow_headers=app_config.cors.allowed_headers,
)


router = APIRouter(prefix="/api/v2")
router.include_router(accounts_router)
router.include_router(clusters_router)
router.include_router(billing_accounts_router)
router.include_router(subscriptions_router)
router.include_router(connections_router)
router.include_router(stream_router)
router.include_router(registration_router)

app.include_router(router)
app.include_router(oauth_router)


@router.get("/health")
async def health_check():
    return {"status": "ok"}

@app.middleware("http")
async def x_subject_id_token_middleware(request: Request, call_next):
    return await x_subject_id_token_middleware_(request, call_next)
