from fastapi import APIRouter, FastAPI
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
