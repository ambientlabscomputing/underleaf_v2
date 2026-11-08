import asyncio

from fastapi import APIRouter, FastAPI, Request
from fastapi.middleware.cors import CORSMiddleware

from cloud_api import app_config
from cloud_api.interface.accounts.accounts import router as accounts_router
from cloud_api.interface.assertion.assertion import router as assertion_router
from cloud_api.interface.billing_accounts.billing_accounts import (
    router as billing_accounts_router,
)
from cloud_api.interface.clusters.clusters import router as clusters_router
from cloud_api.interface.connections.connections import router as connections_router
from cloud_api.interface.deps import (
    x_subject_id_token_middleware_,
)
from cloud_api.interface.oauth.oauth import router as oauth_router
from cloud_api.interface.plan_prices.plan_prices import router as plan_prices_router
from cloud_api.interface.plans.plans import router as plans_router
from cloud_api.interface.registration.registration import router as registration_router
from cloud_api.interface.streams.streams import router as stream_router
from cloud_api.interface.stripe_webhooks.stripe_webhooks import (
    router as stripe_webhooks_router,
)
from cloud_api.interface.subscriptions.subscriptions import (
    router as subscriptions_router,
)
from cloud_api.lib.tls_lib import ensure_server_cert
from cloud_api.service.usage_sync_loop import run_usage_sync_loop
from cloud_api.service_manager import (
    get_assertion_lib,
    get_auth_service,
    get_cert_lib,
)

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
    # Same for the dedicated assertion keys: create them at startup so the
    # first key set fetch never races key generation.
    get_assertion_lib()
    # nginx's server certificate is issued from the CA here, so nginx never
    # needs the CA key (see lib/tls_lib.py). Done after AuthLib has created the
    # CA files.
    if app_config.tls.enabled:
        ensure_server_cert(get_cert_lib(), app_config.tls)


_usage_sync_task: asyncio.Task | None = None


@app.on_event("startup")
async def start_usage_sync_loop():
    global _usage_sync_task
    if app_config.billing.usage_sync_enabled:
        _usage_sync_task = asyncio.create_task(
            run_usage_sync_loop(app_config.billing.usage_sync_interval_seconds)
        )


@app.on_event("shutdown")
async def stop_usage_sync_loop():
    if _usage_sync_task is not None:
        _usage_sync_task.cancel()


router = APIRouter(prefix=app_config.api.base_path)
router.include_router(accounts_router)
router.include_router(clusters_router)
router.include_router(billing_accounts_router)
router.include_router(subscriptions_router)
router.include_router(plans_router)
router.include_router(plan_prices_router)
router.include_router(connections_router)
router.include_router(stream_router)
router.include_router(registration_router)
router.include_router(oauth_router)
router.include_router(assertion_router)
router.include_router(stripe_webhooks_router)


@router.get("/health")
async def health_check():
    return {"status": "ok"}


app.include_router(router)


@app.middleware("http")
async def x_subject_id_token_middleware(request: Request, call_next):
    return await x_subject_id_token_middleware_(request, call_next)
