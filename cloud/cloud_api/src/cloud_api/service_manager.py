"""
service_manager.py — Application-wide singleton factories.

Every repository, lib, and service is constructed exactly once per process
via @lru_cache and exposed as a FastAPI-compatible Depends() callable.

Usage in a router:
    from cloud_api.service_manager import get_account_service
    from cloud_api.service.account_service import AccountService

    @router.post("/signup")
    async def signup(svc: AccountService = Depends(get_account_service)):
        ...
"""

from functools import lru_cache

from cloud_api.lib.auth_lib import AuthLib
from cloud_api.lib.conn_worker_client import ConnWorkerClient
from cloud_api.repository.billing_account_repository import BillingAccountRepository
from cloud_api.repository.cluster_repository import ClusterRepository
from cloud_api.repository.connection_repository import ConnectionRepository
from cloud_api.repository.entitlements_bucket_repository import (
    EntitlementsBucketRepository,
)
from cloud_api.repository.invoice_repository import InvoiceRepository
from cloud_api.repository.node_repository import NodeRepository
from cloud_api.repository.plan_price_repository import PlanPriceRepository
from cloud_api.repository.plan_repository import PlanRepository
from cloud_api.repository.principal_account_repository import (
    PrincipalAccountRepository,
)
from cloud_api.repository.stream_repository import StreamRepository
from cloud_api.repository.stripe_webhook_event_repository import (
    StripeWebhookEventRepository,
)
from cloud_api.repository.subscription_repository import SubscriptionRepository
from cloud_api.repository.top_up_repository import TopUpRepository
from cloud_api.repository.usage_alert_repository import UsageAlertRepository
from cloud_api.repository.usage_event_repository import UsageEventRepository
from cloud_api.repository.user_repository import UserRepository
from cloud_api.service.account_service import AccountService
from cloud_api.service.auth_service import AuthService
from cloud_api.service.billing_account_service import BillingAccountService
from cloud_api.service.cluster_service import ClusterService
from cloud_api.service.connection_service import ConnectionService
from cloud_api.service.entitlements_bucket_service import EntitlementsBucketService
from cloud_api.service.invoice_service import InvoiceService
from cloud_api.service.plan_service import PlanService
from cloud_api.service.registration_service import RegistrationService
from cloud_api.service.stream_service import StreamService
from cloud_api.service.stripe_checkout_service import StripeCheckoutService
from cloud_api.service.stripe_webhook_service import StripeWebhookService
from cloud_api.service.subscription_service import SubscriptionService
from cloud_api.service.top_up_service import TopUpService
from cloud_api.service.usage_enforcement_service import UsageEnforcementService
from cloud_api.service.usage_event_service import UsageEventService
from cloud_api.service.usage_summary_service import UsageSummaryService

# ---------------------------------------------------------------------------
# Repository singletons
# All repositories are stateless — they acquire a fresh DB session per call
# from the connection pool, so sharing a single instance is safe.
# ---------------------------------------------------------------------------


@lru_cache
def get_user_repo() -> UserRepository:
    return UserRepository()


@lru_cache
def get_principal_account_repo() -> PrincipalAccountRepository:
    return PrincipalAccountRepository()


@lru_cache
def get_billing_account_repo() -> BillingAccountRepository:
    return BillingAccountRepository()


@lru_cache
def get_usage_event_repo() -> UsageEventRepository:
    return UsageEventRepository()


@lru_cache
def get_subscription_repo() -> SubscriptionRepository:
    return SubscriptionRepository()


@lru_cache
def get_entitlements_bucket_repo() -> EntitlementsBucketRepository:
    return EntitlementsBucketRepository()


@lru_cache
def get_plan_repo() -> PlanRepository:
    return PlanRepository()


@lru_cache
def get_plan_price_repo() -> PlanPriceRepository:
    return PlanPriceRepository()


@lru_cache
def get_usage_alert_repo() -> UsageAlertRepository:
    return UsageAlertRepository()


@lru_cache
def get_top_up_repo() -> TopUpRepository:
    return TopUpRepository()


@lru_cache
def get_invoice_repo() -> InvoiceRepository:
    return InvoiceRepository()


@lru_cache
def get_stripe_webhook_event_repo() -> StripeWebhookEventRepository:
    return StripeWebhookEventRepository()


# ---------------------------------------------------------------------------
# Lib singletons
# ---------------------------------------------------------------------------


@lru_cache
def get_auth_lib() -> AuthLib:
    """AuthLib is expensive to construct (RSA cert load/generate). One instance."""
    return AuthLib(get_user_repo())


@lru_cache
def get_conn_worker_client() -> ConnWorkerClient:
    from cloud_api import app_config

    if not app_config:
        raise RuntimeError("App config not loaded")
    return ConnWorkerClient(target=app_config.conn_worker.grpc_target)


# ---------------------------------------------------------------------------
# Service singletons
# ---------------------------------------------------------------------------


@lru_cache
def get_account_service() -> AccountService:
    return AccountService(
        principal_account_repo=get_principal_account_repo(),
        billing_account_repo=get_billing_account_repo(),
        usage_event_repo=get_usage_event_repo(),
        user_repo=get_user_repo(),
        subscription_repo=get_subscription_repo(),
        entitlements_bucket_repo=get_entitlements_bucket_repo(),
    )


@lru_cache
def get_entitlements_bucket_service() -> EntitlementsBucketService:
    return EntitlementsBucketService(
        entitlements_bucket_repo=get_entitlements_bucket_repo(),
        usage_alert_repo=get_usage_alert_repo(),
    )


@lru_cache
def get_usage_event_service() -> UsageEventService:
    return UsageEventService(usage_event_repo=get_usage_event_repo())


@lru_cache
def get_plan_service() -> PlanService:
    return PlanService(plan_repo=get_plan_repo(), plan_price_repo=get_plan_price_repo())


@lru_cache
def get_stripe_checkout_service() -> StripeCheckoutService:
    from cloud_api import app_config

    if not app_config:
        raise RuntimeError("App config not loaded")
    return StripeCheckoutService(
        billing_account_repo=get_billing_account_repo(),
        plan_price_repo=get_plan_price_repo(),
        stripe_config=app_config.stripe,
    )


@lru_cache
def get_top_up_service() -> TopUpService:
    from cloud_api import app_config

    if not app_config:
        raise RuntimeError("App config not loaded")
    return TopUpService(
        billing_account_repo=get_billing_account_repo(),
        subscription_repo=get_subscription_repo(),
        plan_price_repo=get_plan_price_repo(),
        top_up_repo=get_top_up_repo(),
        usage_alert_repo=get_usage_alert_repo(),
        entitlements_bucket_service=get_entitlements_bucket_service(),
        stripe_config=app_config.stripe,
    )


@lru_cache
def get_usage_enforcement_service() -> UsageEnforcementService:
    return UsageEnforcementService(
        entitlements_bucket_service=get_entitlements_bucket_service(),
        top_up_service=get_top_up_service(),
    )


@lru_cache
def get_usage_summary_service() -> UsageSummaryService:
    return UsageSummaryService(
        billing_account_repo=get_billing_account_repo(),
        subscription_repo=get_subscription_repo(),
        plan_price_repo=get_plan_price_repo(),
        top_up_repo=get_top_up_repo(),
        usage_alert_repo=get_usage_alert_repo(),
        entitlements_bucket_service=get_entitlements_bucket_service(),
    )


@lru_cache
def get_invoice_service() -> InvoiceService:
    return InvoiceService(invoice_repo=get_invoice_repo())


@lru_cache
def get_stripe_webhook_service() -> StripeWebhookService:
    return StripeWebhookService(
        webhook_event_repo=get_stripe_webhook_event_repo(),
        subscription_repo=get_subscription_repo(),
        billing_account_repo=get_billing_account_repo(),
        plan_repo=get_plan_repo(),
        plan_price_repo=get_plan_price_repo(),
        entitlements_bucket_service=get_entitlements_bucket_service(),
        invoice_repo=get_invoice_repo(),
        top_up_service=get_top_up_service(),
    )


@lru_cache
def get_auth_service() -> AuthService:
    return AuthService(
        auth_lib=get_auth_lib(),
        user_repo=get_user_repo(),
    )


@lru_cache
def get_cluster_repo() -> ClusterRepository:
    return ClusterRepository()


@lru_cache
def get_node_repo() -> NodeRepository:
    return NodeRepository()


@lru_cache
def get_cluster_service() -> ClusterService:
    return ClusterService(
        cluster_repo=get_cluster_repo(),
        node_repo=get_node_repo(),
    )


@lru_cache
def get_conn_repo() -> ConnectionRepository:
    return ConnectionRepository()


@lru_cache
def get_stream_repo() -> StreamRepository:
    return StreamRepository()


@lru_cache
def get_connection_service() -> ConnectionService:
    return ConnectionService(
        conn_repo=get_conn_repo(),
        conn_client=get_conn_worker_client(),
        billing_account_repo=get_billing_account_repo(),
        entitlements_bucket_service=get_entitlements_bucket_service(),
    )


@lru_cache
def get_stream_service() -> StreamService:
    return StreamService(
        stream_repo=get_stream_repo(),
        conn_worker_client=get_conn_worker_client(),
        conn_repo=get_conn_repo(),
        billing_account_repo=get_billing_account_repo(),
        entitlements_bucket_service=get_entitlements_bucket_service(),
        usage_event_service=get_usage_event_service(),
        usage_enforcement_service=get_usage_enforcement_service(),
    )


@lru_cache
def get_billing_account_service() -> BillingAccountService:
    return BillingAccountService(billing_account_repo=get_billing_account_repo())


@lru_cache
def get_subscription_service() -> SubscriptionService:
    return SubscriptionService(subscription_repo=get_subscription_repo())


@lru_cache
def get_registration_repo():
    from cloud_api.repository.registration_repository import RegistrationRepository

    return RegistrationRepository()


@lru_cache
def get_cert_lib():
    from cloud_api import app_config
    from cloud_api.lib.cert_lib import CertLib

    if not app_config:
        raise RuntimeError("App config not loaded")
    return CertLib(
        cert_path=app_config.oauth.root_ca_cert_path,
        key_path=app_config.oauth.private_key_path,
    )


@lru_cache
def get_registration_service() -> RegistrationService:
    return RegistrationService(
        registration_repo=get_registration_repo(),
        cluster_repo=get_cluster_repo(),
        cert_lib=get_cert_lib(),
    )


_ = get_auth_lib()
