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

# How long uvicorn keeps an idle HTTP connection open. It must be longer than
# any proxy in front of it keeps an idle upstream connection (nginx: 60 s).
# uvicorn's default is 5 s, which is exactly the interval ufctl and orcli poll
# the device flows at: nginx then reuses a connection uvicorn is closing at that
# very moment and answers the client 502 ("upstream prematurely closed
# connection") on about every other poll.
KEEP_ALIVE_TIMEOUT_SECONDS = 75


if __name__ == "__main__":
    cloud_api.logger.info(
        f"Swagger docs available at http://{cfg.api.host}:{cfg.api.port}/docs"
    )
    cloud_api.logger.info(
        "starting API server ...", extra={"host": cfg.api.host, "port": cfg.api.port}
    )
    uvicorn.run(
        "cloud_api.interface.router:app",
        host=cfg.api.host,
        port=cfg.api.port,
        reload=cfg.reload,
        timeout_keep_alive=KEEP_ALIVE_TIMEOUT_SECONDS,
    )
