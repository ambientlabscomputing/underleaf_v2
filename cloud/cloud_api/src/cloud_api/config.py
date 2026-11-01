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
        # Name the driver: SQLAlchemy 2.1 defaults a bare postgresql:// to
        # psycopg (v3), but we depend on psycopg2.
        return f"postgresql+psycopg2://{self.username}:{self.password}@{self.host}/{self.name}"

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
    dynamic: bool = Field(
        default=False,
        description="Provision conn_worker VMs on demand (connections are queued "
        "and created once a worker is up) instead of using the static "
        "grpc_target",
    )


class StripeConfig(BaseModel):
    api_key: str = Field(
        default="",
        description="Stripe secret API key. Empty by default; must be set via "
        "config/env before any Stripe-backed endpoint is used.",
    )
    webhook_secret: str = Field(
        default="",
        description="Signing secret for verifying Stripe webhook payloads "
        "(stripe.Webhook.construct_event) -- never trust an unverified webhook body.",
    )
    success_url: str = Field(
        default="http://localhost:5173/billing/success",
        description="Where Stripe Checkout redirects on a completed session",
    )
    cancel_url: str = Field(
        default="http://localhost:5173/billing/cancel",
        description="Where Stripe Checkout redirects on a cancelled session",
    )


class BillingConfig(BaseModel):
    usage_sync_enabled: bool = Field(
        default=True,
        description="Run the background loop that meters conn_worker traffic and enforces allowances",
    )
    usage_sync_interval_seconds: int = Field(
        default=30,
        description="How often traffic is metered; also the worst-case overshoot window "
        "before a hard stop (the grace allowance is sized to absorb it)",
    )


class DigitalOceanConfig(BaseModel):
    token: str = Field(
        default="",
        description="DigitalOcean API token (prefer UNDERLEAF_DO_TOKEN)",
    )
    region: str = Field(default="nyc3")
    image: str = Field(default="ubuntu-24-04-x64")
    ssh_key_ids: list[str] = Field(
        default_factory=list,
        description="SSH key ids/fingerprints to install (only needed for debugging)",
    )


class AdminConfig(BaseModel):
    principal_account_ids: list[str] = Field(
        default_factory=list,
        description="Principal accounts whose access tokens may use /admin/*. "
        "Empty (the default) disables the admin API entirely.",
    )


class CloudflareConfig(BaseModel):
    api_token: str = Field(
        default="",
        description="Cloudflare token with Zone:DNS:Edit on the zone "
        "(prefer UNDERLEAF_CLOUDFLARE_TOKEN)",
    )
    zone_id: str = Field(
        default="", description="Zone that holds the worker's DNS names"
    )


class WorkerDefaults(BaseModel):
    cpu: int = Field(
        default=2,
        description="Default number of CPU cores for a worker",
    )
    mem_gb: int = Field(
        default=4,
        description="Default amount of memory (in GB) for a worker",
    )
    storage_gb: int = Field(
        default=20,
        description="Default amount of storage (in GB) for a worker",
    )
    provider: str = Field(
        default="digital_ocean",
        description="Default cloud provider for worker VMs",
    )
    image: str = Field(
        default="",
        description="Container image of conn_worker the VM pulls and runs "
        "(e.g. ghcr.io/<org>/underleaf-conn-worker:<tag>). Required to provision.",
    )
    environment: str = Field(
        default="prod",
        description="Tag value separating environments sharing one cloud account; "
        "the orphan reaper only touches VMs tagged with its own",
    )
    provisioning_enabled: bool = Field(
        default=True,
        description="Kill switch: when false no new worker VMs are created "
        "(existing ones keep being managed and torn down)",
    )
    reconciler_enabled: bool = Field(
        default=True,
        description="Run the background loop that advances, health-checks and "
        "reaps worker VMs",
    )
    task_poll_seconds: float = Field(
        default=5.0,
        description="How often the task runner re-checks queued work when no "
        "wake-up event arrives",
    )
    task_timeout_seconds: int = Field(
        default=900,
        description="A queued connection that still has no worker after this "
        "long is failed and its slot released",
    )
    task_max_attempts: int = Field(
        default=5, description="Attempts to create a connection on a ready worker"
    )
    task_retry_backoff_seconds: int = Field(
        default=5, description="Base of the exponential backoff between attempts"
    )
    reconcile_interval_seconds: int = Field(
        default=10, description="Seconds between worker reconciler ticks"
    )
    boot_timeout_seconds: int = Field(
        default=600,
        description="Max time a worker may spend waiting for its VM to boot and "
        "bootstrap before it is torn down and retried",
    )
    idle_grace_seconds: int = Field(
        default=600,
        description="How long a worker may sit with no open connections before "
        "it is drained",
    )
    drain_seconds: int = Field(
        default=30,
        description="Time a worker spends DRAINING (a new request cancels the "
        "drain) before its VM is deleted",
    )
    unhealthy_grace_seconds: int = Field(
        default=120,
        description="How long an AVAILABLE worker may be unreachable before it "
        "is declared failed and replaced",
    )
    max_vm_lifetime_hours: int = Field(
        default=72,
        description="Cost backstop: a VM older than this is drained even if busy "
        "(0 disables)",
    )
    max_provision_attempts: int = Field(
        default=3, description="Attempts at one step before the worker is given up on"
    )
    breaker_threshold: int = Field(
        default=3,
        description="Circuit breaker: failed workers within the window that "
        "stop new provisioning",
    )
    breaker_window_seconds: int = Field(default=900)
    orphan_min_age_seconds: int = Field(
        default=300,
        description="A tagged VM with no live worker is only reaped once this old",
    )
    api_public_url: str = Field(
        default="",
        description="Public base URL of this API as the VM sees it, e.g. "
        "https://underleafapp.com/api/v2/cloud. The VM calls back here.",
    )
    api_egress_ip: str = Field(
        default="",
        description="Public IP the API reaches workers from. When set, the VM "
        "firewall only admits gRPC from it",
    )
    gateway_domain: str = Field(
        default="underleafapp.com",
        description="Gateway domain written into the worker's conn_worker config",
    )
    tunnel_hostname: str = Field(
        default="",
        description="Stable DNS name agents dial; added to the worker's TLS cert SANs",
    )
    hourly_cost_usd: float = Field(
        default=0.0,
        description="What one worker VM costs per hour (look it up for your "
        "size/provider). Used only for the /admin/cost estimate; 0 = unknown.",
    )
    dns_names: list[str] = Field(
        default_factory=list,
        description="A records kept pointed at the ready worker, e.g. "
        '["*.gw.underleafapp.com"]; tunnel_hostname is always included. '
        "Needs cloudflare.api_token and zone_id.",
    )
    cloudflare: CloudflareConfig = Field(default_factory=CloudflareConfig)
    grpc_tls_port: int = Field(
        default=50443, description="Public mTLS port fronting the worker's gRPC"
    )
    digital_ocean: "DigitalOceanConfig" = Field(
        default_factory=lambda: DigitalOceanConfig()
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
    stripe: StripeConfig = Field(
        default_factory=StripeConfig, description="Stripe integration settings"
    )
    billing: BillingConfig = Field(
        default_factory=BillingConfig,
        description="Usage metering / enforcement settings",
    )
    worker_defaults: WorkerDefaults = Field(
        default_factory=WorkerDefaults,
        description="Default configuration for workers",
    )
    admin: AdminConfig = Field(
        default_factory=AdminConfig, description="Operator (admin API) access"
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
    # Secrets come from the environment so they never have to be committed in
    # a config file; an env var wins over the file.
    if os.getenv("UNDERLEAF_DO_TOKEN"):
        app_config.worker_defaults.digital_ocean.token = os.environ[
            "UNDERLEAF_DO_TOKEN"
        ]
    if os.getenv("UNDERLEAF_CLOUDFLARE_TOKEN"):
        app_config.worker_defaults.cloudflare.api_token = os.environ[
            "UNDERLEAF_CLOUDFLARE_TOKEN"
        ]
    if os.getenv("UNDERLEAF_STRIPE_API_KEY"):
        app_config.stripe.api_key = os.environ["UNDERLEAF_STRIPE_API_KEY"]
    if os.getenv("UNDERLEAF_STRIPE_WEBHOOK_SECRET"):
        app_config.stripe.webhook_secret = os.environ["UNDERLEAF_STRIPE_WEBHOOK_SECRET"]
    return app_config
