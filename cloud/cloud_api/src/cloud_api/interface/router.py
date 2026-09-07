from cloud_api import app_config
from cloud_api.interface.accounts.accounts import router as accounts_router
from cloud_api.interface.billing_accounts.billing_accounts import (
    router as billing_accounts_router,
)
from cloud_api.interface.clusters.clusters import router as clusters_router
from cloud_api.interface.connections.connections import router as connections_router
from cloud_api.interface.deps import (
    x_subject_id_token_middleware_,
)
from cloud_api.interface.oauth.oauth import router as oauth_router
from cloud_api.interface.registration.registration import router as registration_router
from cloud_api.interface.streams.streams import router as stream_router
from cloud_api.interface.subscriptions.subscriptions import (
    router as subscriptions_router,
)
from cloud_api.service_manager import get_auth_service
from fastapi import APIRouter, FastAPI, Request
from fastapi.middleware.cors import CORSMiddleware

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


@app.on_event("startup")
async def bootstrap_certs_on_startup():
    # AuthLib.__init__ self-provisions the root CA cert/key (if missing) at
    # app_config.oauth.root_ca_cert_path / private_key_path. Force
    # construction eagerly on process startup -- rather than lazily on the
    # first auth request -- so the shared cert files are guaranteed to exist
    # as soon as this container is up (nginx and other consumers mount the
    # same volume and need those files present immediately).
    get_auth_service()


router = APIRouter(prefix=app_config.api.base_path)
router.include_router(accounts_router)
router.include_router(clusters_router)
router.include_router(billing_accounts_router)
router.include_router(subscriptions_router)
router.include_router(connections_router)
router.include_router(stream_router)
router.include_router(registration_router)
router.include_router(oauth_router)


@router.get("/health")
async def health_check():
    return {"status": "ok"}


app.include_router(router)


@app.middleware("http")
async def x_subject_id_token_middleware(request: Request, call_next):
    return await x_subject_id_token_middleware_(request, call_next)
