from fastapi import APIRouter, FastAPI
from fastapi.middleware.cors import CORSMiddleware
from cloud_api import app_config
from cloud_api.interface.accounts.accounts import router as accounts_router
from cloud_api.interface.oauth.oauth import router as oauth_router

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
app.include_router(router)
app.include_router(oauth_router)
app.include_router(accounts_router)


@router.get("/health")
async def health_check():
    return {"status": "ok"}
