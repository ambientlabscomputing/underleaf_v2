"""Shared fixtures for integration tests that need Postgres and a fake
conn_worker gRPC stub (real ConnWorkerClient code, fake wire)."""

import uuid
from datetime import UTC, datetime
from types import SimpleNamespace

import pytest
import pytest_asyncio
from google.protobuf import empty_pb2
from sqlalchemy import text

import cloud_api
from cloud_api import service_manager
from cloud_api.lib.conn_worker_client import conn_worker_pb2 as pb2
from cloud_api.lib.conn_worker_client.conn_worker_client import ConnWorkerClient
from cloud_api.models.api import CreateUserRequest
from cloud_api.repository import session as db_session


class FakeConnWorkerStub:
    """Speaks the same protobuf messages the real conn_worker does --
    including its quirks (no `name` on Stream, empty-string timestamps and
    optionals, TerminateConnection returning Empty)."""

    def __init__(self):
        self.connections: dict[str, pb2.Connection] = {}
        self.streams: dict[str, pb2.Stream] = {}
        self._n = 0

    def _id(self, prefix: str) -> str:
        self._n += 1
        return f"{prefix}_fake{uuid.uuid4().hex[:10]}{self._n}"

    async def CreateConnection(self, req):
        conn = pb2.Connection(
            id=self._id("conn"),
            node_id=req.node_id,
            name=req.name,
            state="provisioned",
            status="succeeded",
        )
        self.connections[conn.id] = conn
        return conn

    async def TerminateConnection(self, req):
        self.connections.pop(req.id, None)
        return empty_pb2.Empty()

    async def ListConnections(self, _empty):
        for conn in list(self.connections.values()):
            yield conn

    async def NewStream(self, req):
        stream = pb2.Stream(
            id=self._id("stream"),
            connection_id=req.connection_id,
            type=req.type,
            state="active",
            status="succeeded",
            port=req.port,
            endpoint=req.endpoint,
        )
        self.streams[stream.id] = stream
        return stream

    async def CloseStream(self, req):
        stream = self.streams.get(req.stream_id)
        if stream is not None:
            stream.state = "closed"
        return pb2.CloseStreamResponse(id=req.stream_id, status="succeeded")

    async def ListStreams(self, _empty):
        for stream in list(self.streams.values()):
            yield stream

    async def GetStream(self, req):
        return self.streams[req.id]

    async def GetConnection(self, req):
        return self.connections[req.id]


@pytest_asyncio.fixture
async def db():
    db_session.init_session_maker(cloud_api.app_config)
    engine = db_session.get_engine()
    try:
        async with engine.connect() as conn:
            await conn.execute(text("SELECT 1 FROM underleaf.usage_events LIMIT 1"))
    except Exception as exc:  # noqa: BLE001
        await engine.dispose()
        pytest.skip(f"Postgres with migrations not available: {exc}")
    yield
    await engine.dispose()


@pytest.fixture
def fake_stub():
    return FakeConnWorkerStub()


@pytest.fixture
def wired_services(monkeypatch, db, fake_stub):
    """service_manager singletons rebuilt around a real ConnWorkerClient
    whose gRPC stub is the fake."""
    client = ConnWorkerClient(target="fake")
    client._channel = SimpleNamespace()
    client._stub = fake_stub
    monkeypatch.setattr(service_manager, "get_conn_worker_client", lambda: client)
    for factory in (
        service_manager.get_connection_service,
        service_manager.get_stream_service,
    ):
        factory.cache_clear()
    yield service_manager
    for factory in (
        service_manager.get_connection_service,
        service_manager.get_stream_service,
    ):
        factory.cache_clear()


async def _make_tenant():
    """A signed-up account (principal + billing account + subscription +
    free bucket) with one cluster and one node."""
    suffix = uuid.uuid4().hex[:8]
    signup = await service_manager.get_account_service().sign_up(
        CreateUserRequest(
            name=f"it-{suffix}", email=f"it-{suffix}@example.com", password="pw-1234"
        )
    )
    cluster_id = f"cluster_it_{suffix}"
    node_id = f"node_it_{suffix}"
    now = datetime.now(UTC)
    async with db_session.get_session() as s:
        await s.execute(
            text(
                "INSERT INTO underleaf.clusters "
                "(id, name, principal_account_id, created_at, updated_at) "
                "VALUES (:id, 'c', :pid, :now, :now)"
            ),
            {"id": cluster_id, "pid": signup.principal_account.id, "now": now},
        )
        await s.execute(
            text(
                "INSERT INTO underleaf.nodes "
                "(id, name, cluster_id, created_at, updated_at) "
                "VALUES (:id, 'n', :cid, :now, :now)"
            ),
            {"id": node_id, "cid": cluster_id, "now": now},
        )
        await s.commit()
    return SimpleNamespace(
        principal_id=signup.principal_account.id,
        billing_account_id=signup.billing_account.id,
        node_id=node_id,
        cluster_id=cluster_id,
    )


@pytest_asyncio.fixture
async def tenant(db):
    return await _make_tenant()


@pytest_asyncio.fixture
async def other_tenant(db):
    """A second, unrelated tenant for cross-tenant access tests."""
    return await _make_tenant()


# ---------------------------------------------------------------------------
# Helpers for overage-policy scenarios
# ---------------------------------------------------------------------------

GB = 10**9


async def put_on_plan(tenant, plan_name: str, *, auto_top_up=False, cap_cents=5000):
    """Move a tenant onto a paid plan through the real webhook path
    (checkout.session.completed), exactly as Stripe would drive it."""
    from cloud_api.models.api import (
        InternalPatchBillingAccountRequest,
        QueryPlanPriceRequest,
        QueryPlanRequest,
    )
    from tests.helpers import stripe_event

    sm = service_manager
    plan = (await sm.get_plan_repo().query_plans(QueryPlanRequest(name=plan_name)))[0]
    price = (
        await sm.get_plan_price_repo().query_plan_prices(
            QueryPlanPriceRequest(plan_id=plan.id)
        )
    )[0]
    suffix = uuid.uuid4().hex[:10]
    tenant.stripe_customer_id = f"cus_{suffix}"
    await sm.get_billing_account_repo().patch_billing_account(
        tenant.billing_account_id,
        InternalPatchBillingAccountRequest(
            stripe_customer_id=tenant.stripe_customer_id,
            auto_top_up_enabled=auto_top_up,
            monthly_top_up_cap_cents=cap_cents,
        ),
    )
    tenant.stripe_subscription_id = f"sub_{suffix}"
    tenant.plan_price = price
    await sm.get_stripe_webhook_service().handle_event(
        stripe_event(
            f"evt_checkout_{suffix}",
            "checkout.session.completed",
            {
                "metadata": {
                    "billing_account_id": tenant.billing_account_id,
                    "plan_price_id": price.id,
                },
                "subscription": tenant.stripe_subscription_id,
            },
        )
    )
    return price


@pytest.fixture
def stub_stripe_charge(monkeypatch):
    """Replace only the Stripe network call; everything around it (cap,
    pending guard, persistence, credit-on-webhook) stays real."""
    from cloud_api.service.top_up_service import TopUpService

    # Billing refuses to act without a key; the value is never sent anywhere
    # because _charge_via_stripe is replaced below.
    monkeypatch.setattr(cloud_api.app_config.stripe, "api_key", "sk_test_stubbed")
    calls = []

    def fake_charge(self, account, price, top_up):
        calls.append(top_up.id)
        return f"in_topup_{top_up.id}"

    monkeypatch.setattr(TopUpService, "_charge_via_stripe", fake_charge)
    return calls


async def open_stream(wired_services, tenant, fake_stub):
    """A connection plus one active gateway stream, returned as the stream."""
    from cloud_api.models.api import CreateConnectionRequest, NewStreamRequest

    conn = await wired_services.get_connection_service().create_connection(
        CreateConnectionRequest(name="c", node_id=tenant.node_id)
    )
    return await wired_services.get_stream_service().create_stream(
        NewStreamRequest(connection_id=conn.id, type="gateway", port=8080)
    )


async def meter(wired_services, fake_stub, stream_id: str, total_bytes: int):
    """Report `total_bytes` cumulative on the worker side and run one usage
    sync tick."""
    fake_stub.streams[stream_id].bytes_in = total_bytes
    await wired_services.get_stream_service().sync_usage_from_conn_worker()


async def usage(wired_services, tenant):
    return await wired_services.get_usage_summary_service().get_summary(
        tenant.billing_account_id
    )


@pytest_asyncio.fixture
async def upgrade_target(db):
    """A throwaway, inactive plan + Stripe-linked price to upgrade onto.

    Deliberately NOT the seeded Builder row: the seeded catalog is shared
    (and carries real `stripe-sync` links), so tests must never mutate it --
    concurrent runs would race on it. Inactive, so it never shows up in the
    Plans page.
    """
    from cloud_api.models.api import CreatePlanPriceRequest, CreatePlanRequest

    sm = service_manager
    suffix = uuid.uuid4().hex[:8]
    plan = await sm.get_plan_repo().create_plan(
        CreatePlanRequest(name=f"it_plan_{suffix}", is_active=False)
    )
    stripe_price_id = f"price_it_{suffix}"
    await sm.get_plan_price_repo().create_plan_price(
        CreatePlanPriceRequest(
            plan_id=plan.id,
            stripe_price_id=stripe_price_id,
            interval="month",
            unit_amount_cents=2400,
            is_active=False,
            network_traffic_limit_bytes=500 * GB,
            connection_slot_limit=25,
            grace_bytes=10 * GB,
        )
    )
    return stripe_price_id
