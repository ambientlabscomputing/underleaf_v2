"""Integration tests that run the billing SQL against a real Postgres.

The mock-based service tests can't prove the guarantees that live in SQL
(atomic decrement, unique-constraint idempotency, ON CONFLICT upserts), so
these hit the DB brought up by `make db-up` with migrations applied
(`alembic upgrade head`). They skip automatically when Postgres isn't
reachable, so the default `pytest` run stays hermetic.
"""

import asyncio
import uuid
from datetime import UTC, datetime

import pytest
import pytest_asyncio
from sqlalchemy import text

import cloud_api
from cloud_api.models.api import (
    CreateBillingAccountRequest,
    CreateEntitlementsBucketRequest,
    CreatePrincipalAccountRequest,
    CreateUsageEventRequest,
    PatchEntitlementsBucketRequest,
    UpsertInvoiceRequest,
    UsageEventType,
    UsageUnits,
)
from cloud_api.repository import session as db_session
from cloud_api.repository.billing_account_repository import BillingAccountRepository
from cloud_api.repository.entitlements_bucket_repository import (
    EntitlementsBalanceField,
    EntitlementsBucketRepository,
)
from cloud_api.repository.invoice_repository import InvoiceRepository
from cloud_api.repository.principal_account_repository import (
    PrincipalAccountRepository,
)
from cloud_api.repository.stripe_webhook_event_repository import (
    StripeWebhookEventRepository,
)
from cloud_api.repository.usage_event_repository import UsageEventRepository


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


@pytest_asyncio.fixture
async def billing_account(db):
    principal = await PrincipalAccountRepository().create_principal_account(
        CreatePrincipalAccountRequest(name=f"it-{uuid.uuid4().hex[:8]}")
    )
    return await BillingAccountRepository().create_billing_account(
        CreateBillingAccountRequest(
            name="it-billing", principal_account_id=principal.id
        )
    )


async def _make_bucket(billing_account_id: str, network: int, slots: int):
    repo = EntitlementsBucketRepository()
    bucket = await repo.create_entitlements_bucket(
        CreateEntitlementsBucketRequest(
            name="it-bucket", billing_account_id=billing_account_id
        )
    )
    return await repo.patch_entitlements_bucket(
        bucket.id,
        PatchEntitlementsBucketRequest(
            network_traffic_balance=network, connection_slot_balance=slots
        ),
    )


@pytest.mark.asyncio
async def test_concurrent_decrements_never_overspend(billing_account):
    repo = EntitlementsBucketRepository()
    bucket = await _make_bucket(billing_account.id, network=0, slots=10)

    results = await asyncio.gather(
        *[
            repo.decrement_balance(
                bucket.id, EntitlementsBalanceField.CONNECTION_SLOT, 1
            )
            for _ in range(40)
        ]
    )

    succeeded = [r for r in results if r is not None]
    assert len(succeeded) == 10
    final = await repo.get_entitlements_bucket(bucket.id)
    assert final.connection_slot_balance == 0


@pytest.mark.asyncio
async def test_decrement_handles_byte_counts_beyond_int32(billing_account):
    repo = EntitlementsBucketRepository()
    ten_gib = 10 * 1024**3
    bucket = await _make_bucket(billing_account.id, network=ten_gib, slots=0)

    updated = await repo.decrement_balance(
        bucket.id, EntitlementsBalanceField.NETWORK_TRAFFIC, 3 * 1024**3
    )

    assert updated.network_traffic_balance == 7 * 1024**3


@pytest.mark.asyncio
async def test_usage_event_idempotency_key_dedupes(billing_account):
    repo = UsageEventRepository()
    now = datetime.now(UTC)
    req = CreateUsageEventRequest(
        billing_account_id=billing_account.id,
        event_type=UsageEventType.DATA_USAGE,
        usage_unit=UsageUnits.BYTES,
        usage_amount=5 * 1024**3,
        period_start=now,
        period_end=now,
        idempotency_key=f"it-{uuid.uuid4().hex}",
    )

    first, second = await asyncio.gather(
        repo.create_usage_event(req), repo.create_usage_event(req)
    )

    assert first.id == second.id
    async with db_session.get_session() as s:
        count = (
            await s.execute(
                text(
                    "SELECT count(*) FROM underleaf.usage_events "
                    "WHERE idempotency_key = :k"
                ),
                {"k": req.idempotency_key},
            )
        ).scalar_one()
    assert count == 1


@pytest.mark.asyncio
async def test_webhook_event_record_if_new_only_one_winner(db):
    repo = StripeWebhookEventRepository()
    event_id = f"evt_it_{uuid.uuid4().hex}"

    results = await asyncio.gather(
        *[repo.record_if_new(event_id, "invoice.paid", {"k": "v"}) for _ in range(10)]
    )

    assert results.count(True) == 1
    assert results.count(False) == 9


@pytest.mark.asyncio
async def test_invoice_upsert_updates_in_place(billing_account):
    repo = InvoiceRepository()
    stripe_invoice_id = f"in_it_{uuid.uuid4().hex}"
    base = {
        "billing_account_id": billing_account.id,
        "stripe_invoice_id": stripe_invoice_id,
        "amount_due_cents": 1000,
        "currency": "usd",
    }

    open_inv = await repo.upsert_invoice(
        UpsertInvoiceRequest(status="open", amount_paid_cents=0, **base)
    )
    paid_inv = await repo.upsert_invoice(
        UpsertInvoiceRequest(status="paid", amount_paid_cents=1000, **base)
    )

    assert open_inv.id == paid_inv.id
    assert paid_inv.status == "paid"
    assert paid_inv.amount_paid_cents == 1000


@pytest.mark.asyncio
async def test_billing_account_resolves_from_node_id(billing_account):
    async with db_session.get_session() as s:
        cluster_id = f"cluster_it_{uuid.uuid4().hex[:8]}"
        node_id = f"node_it_{uuid.uuid4().hex[:8]}"
        now = datetime.now(UTC)
        await s.execute(
            text(
                "INSERT INTO underleaf.clusters "
                "(id, name, principal_account_id, created_at, updated_at) "
                "VALUES (:id, 'c', :pid, :now, :now)"
            ),
            {"id": cluster_id, "pid": billing_account.principal_account_id, "now": now},
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

    resolved = await BillingAccountRepository().get_billing_account_by_node_id(node_id)

    assert resolved is not None
    assert resolved.id == billing_account.id
