import uvicorn
import cloud_api
from cloud_api import load_config
from cloud_api.repository.session import init_session_maker

cfg = load_config()
if not cfg:
    raise RuntimeError("Failed to load app config")

# load_config() updates cloud_api.config.app_config, but any module that has
# already done `from cloud_api import app_config` holds a None snapshot.
# Writing back to the package attribute ensures modules imported below get
# the live AppConfig instance instead of None.
cloud_api.app_config = cfg

init_session_maker(cfg)

from cloud_api.interface.router import app  # noqa: E402 — must import after config + session init

if __name__ == "__main__":
    print(f"Swagger docs available at http://{cfg.api.host}:{cfg.api.port}/docs")
    uvicorn.run(
        "cloud_api.interface.router:app",
        host=cfg.api.host,
        port=cfg.api.port,
        reload=cfg.reload,
    )

