import os
import pathlib

import yaml
from pydantic import BaseModel, Field


def _repo_root_cert_path(filename: str) -> str:
    """Resolve a cert filename relative to the repo-root certs/ dir.

    Anchored to this file's on-disk location rather than the process cwd, so
    bare-metal `make run` (cwd=cloud/cloud_api, src-layout: .../src/cloud_api/config.py)
    resolves to the same repo-root certs/ dir Docker uses, instead of falling
    back to a different `./certs` relative to wherever the process happened
    to start. Walks up looking for a repo-root marker rather than assuming a
    fixed depth, since the installed layout differs: Docker's image flattens
    this to /app/cloud_api/config.py (no src/ layer, no .git or certs/
    anywhere above it), where a fixed parents[N] index would go out of range.
    Docker's actual runtime path always comes from
    configs/local/cloud_api.yaml (mounted to /var/lib/underleaf/certs)
    regardless of this default, so falling back to the original relative
    path there is harmless -- it's overwritten before use.
    """
    for ancestor in pathlib.Path(__file__).resolve().parents:
        if (ancestor / ".git").exists() or (ancestor / "certs").is_dir():
            return str(ancestor / "certs" / filename)
    return f"./certs/{filename}"


class DBConfig(BaseModel):
    host: str = Field(
        default="localhost:5432",
        description="Database host address, include port if any",
    )
    name: str = Field(
        default="postgres", description="Name of the database to connect to"
    )
    username: str = Field(
        default="postgres", description="Username for database authentication"
    )
    password: str = Field(
        default="postgres", description="Password for database authentication"
    )
    log_sql: bool = Field(
        default=False, description="Whether to enable SQL query logging for debugging"
    )

    @property
    def sync_connection_string(self) -> str:
        """Constructs the database connection string."""
        return f"postgresql://{self.username}:{self.password}@{self.host}/{self.name}"

    @property
    def async_connection_string(self) -> str:
        """Constructs the asynchronous database connection string."""
        return f"postgresql+asyncpg://{self.username}:{self.password}@{self.host}/{self.name}"


class LogConfig(BaseModel):
    level: str = Field(
        default="DEBUG",
        description="Logging level (e.g., DEBUG, INFO, WARNING, ERROR, CRITICAL)",
    )
    log_location: str = Field(
        default="/tmp/cloud_api.log",
        description="File path to write logs to. If empty, logs will be written to stdout",
    )


class OAuthConfig(BaseModel):
    admin_email: str = Field(
        default="hello@ambienlabs.io",
        description="Email address of the administrator user for the OAuth system",
    )
    # Tokens
    access_token_expiration_minutes: int = Field(
        default=60, description="Expiration time for access tokens in minutes"
    )
    refresh_token_expiration_days: int = Field(
        default=30, description="Expiration time for refresh tokens in days"
    )
    issuer: str = Field(
        default="underleaf",
        description="Issuer identifier for the OAuth tokens, typically the name of the service",
    )
    audience: str = Field(
        default="underleaf",
        description="Audience identifier for the OAuth tokens, typically the intended recipients of the token",
    )
    # Certs
    root_ca_cert_path: str = Field(
        default_factory=lambda: _repo_root_cert_path("root_ca_cert.pem"),
        description="File path to the root CA certificate for validating OAuth tokens",
    )
    private_key_path: str = Field(
        default_factory=lambda: _repo_root_cert_path("private_key.pem"),
        description="File path to the private key for signing OAuth tokens",
    )
    bootstrap_certs: bool = Field(
        default=True,
        description="Whether to generate a new root CA certificate and private key if they don't exist at the specified path",
    )


class CORSConfig(BaseModel):
    allowed_origins: list[str] = Field(
        default=["*"],
        description="List of allowed origins for CORS. Use ['*'] to allow all origins.",
    )
    allowed_methods: list[str] = Field(
        default=["*"],
        description="List of allowed HTTP methods for CORS. Use ['*'] to allow all methods.",
    )
    allowed_headers: list[str] = Field(
        default=["*"],
        description="List of allowed HTTP headers for CORS. Use ['*'] to allow all headers.",
    )


class APIConfig(BaseModel):
    host: str = Field(
        default="0.0.0.0", description="Host address for the API server to bind to"
    )
    port: int = Field(
        default=8080, description="Port number for the API server to listen on"
    )
    base_path: str = Field(
        default="/api/v2/cloud", description="Base path prefix for all API endpoints"
    )


class ConnWorkerConfig(BaseModel):
    grpc_target: str = Field(
        default="localhost:50102",
        description="gRPC target address for the connection worker service",
    )


class AppConfig(BaseModel):
    reload: bool = Field(
        default=True,
        description="Whether to enable auto-reloading of the API server on code changes (for development)",
    )
    account_ui_base_url: str = Field(
        default="http://localhost:5173",
        description="Base URL for the account UI — used to build the device-auth verification_uri",
    )
    db: DBConfig = Field(
        default_factory=DBConfig, description="Database configuration settings"
    )
    log: LogConfig = Field(
        default_factory=LogConfig, description="Logging configuration settings"
    )
    oauth: OAuthConfig = Field(
        default_factory=OAuthConfig, description="OAuth configuration settings"
    )
    cors: CORSConfig = Field(
        default_factory=CORSConfig, description="CORS configuration settings"
    )
    api: APIConfig = Field(
        default_factory=APIConfig, description="API server configuration settings"
    )
    conn_worker: ConnWorkerConfig = Field(
        default_factory=ConnWorkerConfig,
        description="Connection worker service configuration settings",
    )


app_config: AppConfig | None = None
CONFIG_FILE_PATH = os.getenv("UNDERLEAF_CONFIG") or "./config.yaml"


def load_config() -> AppConfig:
    """Loads the application configuration from a YAML file."""
    global app_config
    # set default config
    app_config = AppConfig()
    # check if config file exists
    config_path = pathlib.Path(CONFIG_FILE_PATH)
    if config_path.exists():
        with open(config_path, "r") as f:
            config_data = yaml.safe_load(f)
        # merge the loaded config with the default config
        app_config = AppConfig(**{**app_config.model_dump(), **config_data})
    return app_config
