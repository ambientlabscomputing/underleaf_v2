"""The pricing / overage policy (RFDs/RFD-4.md) end to end against a real
database: real webhook handling, usage sync, enforcement and top-up code, with
only the Stripe network call and the conn_worker wire stubbed.

Each test is one row of the policy table.
"""

import uuid

import pytest
from sqlalchemy import text

from cloud_api.models.api import (
    CreateConnectionRequest,
    NewStreamRequest,
    StreamState,
    TopUpStatus,
    UsageAlertKind,
)
from cloud_api.repository import session as db_session
from cloud_api.service.stream_service import InsufficientNetworkTrafficBalanceError
from tests.helpers import stripe_event
from tests.integration.conftest import (
    GB,
    meter,
    open_stream,
    put_on_plan,
    usage,
)


def kinds(summary) -> set[str]:
    return {a.kind.value for a in summary.alerts}


async def stream_state(wired_services, stream_id) -> StreamState:
    return (
        await wired_services.get_stream_service().stream_repo.get_stream(stream_id)
    ).state


# -- Free tier ----------------------------------------------------------------


@pytest.mark.asyncio
async def test_free_user_reaching_allowance_is_hard_stopped(
    wired_services, tenant, fake_stub
):
    stream = await open_stream(wired_services, tenant, fake_stub)

    await meter(wired_services, fake_stub, stream.id, 5 * GB)

    s = await usage(wired_services, tenant)
    assert s.paid is False
    assert s.blocked is True and s.grace_active is False  # free has no grace
    assert await stream_state(wired_services, stream.id) == StreamState.CLOSED
    assert fake_stub.streams[stream.id].state == "closed"  # stopped at the worker
    with pytest.raises(InsufficientNetworkTrafficBalanceError):
        await wired_services.get_stream_service().create_stream(
            NewStreamRequest(connection_id=stream.connection_id, type="gateway")
        )


@pytest.mark.asyncio
async def test_free_user_below_allowance_is_not_notified_or_stopped(
    wired_services, tenant, fake_stub
):
    stream = await open_stream(wired_services, tenant, fake_stub)

    await meter(wired_services, fake_stub, stream.id, 4_500_000_000)  # 90%

    s = await usage(wired_services, tenant)
    assert s.blocked is False
    assert kinds(s) == set()  # 80/95% nudges are for paid plans only
    assert await stream_state(wired_services, stream.id) == StreamState.ACTIVE


@pytest.mark.asyncio
async def test_free_allowance_refills_lazily_next_month(
    wired_services, tenant, fake_stub
):
    stream = await open_stream(wired_services, tenant, fake_stub)
    await meter(wired_services, fake_stub, stream.id, 5 * GB)
    assert (await usage(wired_services, tenant)).blocked

    async with db_session.get_session() as s:
        await s.execute(
            text(
                "UPDATE underleaf.entitlements_buckets "
                "SET period_end = now() - interval '1 day' WHERE billing_account_id = :b"
            ),
            {"b": tenant.billing_account_id},
        )
        await s.commit()

    after = await usage(wired_services, tenant)
    assert after.blocked is False
    assert after.remaining_bytes == 5 * GB


# -- Paid: notifications ---------------------------------------------------------


@pytest.mark.asyncio
async def test_paid_user_gets_80_then_95_notifications_once(
    wired_services, tenant, fake_stub
):
    await put_on_plan(tenant, "maker")  # 100 GB
    stream = await open_stream(wired_services, tenant, fake_stub)

    await meter(wired_services, fake_stub, stream.id, 79 * GB)
    assert kinds(await usage(wired_services, tenant)) == set()

    await meter(wired_services, fake_stub, stream.id, 80 * GB)
    assert kinds(await usage(wired_services, tenant)) == {"usage_80"}

    await meter(wired_services, fake_stub, stream.id, 96 * GB)
    s = await usage(wired_services, tenant)
    assert kinds(s) == {"usage_80", "usage_95"}

    # Staying above a threshold must not re-raise it.
    await meter(wired_services, fake_stub, stream.id, 97 * GB)
    assert len((await usage(wired_services, tenant)).alerts) == 2


# -- Paid: 100%, grace, stop -----------------------------------------------------


@pytest.mark.asyncio
async def test_paid_user_at_100_percent_gets_grace_then_stops(
    wired_services, tenant, fake_stub
):
    await put_on_plan(tenant, "maker")  # 100 GB, grace 2 GB
    stream = await open_stream(wired_services, tenant, fake_stub)

    await meter(wired_services, fake_stub, stream.id, 100 * GB)

    s = await usage(wired_services, tenant)
    assert s.grace_active is True and s.blocked is False
    assert s.grace_remaining_bytes == 2 * GB
    assert "usage_100" in kinds(s)
    assert await stream_state(wired_services, stream.id) == StreamState.ACTIVE
    # New streams are still allowed while on grace.
    await wired_services.get_stream_service().create_stream(
        NewStreamRequest(connection_id=stream.connection_id, type="gateway")
    )

    await meter(wired_services, fake_stub, stream.id, 101 * GB)  # half the grace
    s = await usage(wired_services, tenant)
    assert s.grace_remaining_bytes == 1 * GB and s.blocked is False

    await meter(wired_services, fake_stub, stream.id, 102 * GB)  # grace spent
    s = await usage(wired_services, tenant)
    assert s.blocked is True  # auto top-up disabled -> relayed traffic stops
    assert await stream_state(wired_services, stream.id) == StreamState.CLOSED
    with pytest.raises(InsufficientNetworkTrafficBalanceError):
        await wired_services.get_stream_service().create_stream(
            NewStreamRequest(connection_id=stream.connection_id, type="gateway")
        )


@pytest.mark.asyncio
async def test_grace_is_granted_only_once_per_exhaustion(
    wired_services, tenant, fake_stub
):
    await put_on_plan(tenant, "maker")
    stream = await open_stream(wired_services, tenant, fake_stub)
    await meter(wired_services, fake_stub, stream.id, 100 * GB)
    await meter(wired_services, fake_stub, stream.id, 102 * GB)  # spend it all
    assert (await usage(wired_services, tenant)).blocked

    # More traffic from a stream that somehow survived must not re-grant.
    fake_stub.streams[stream.id].state = "active"
    await meter(wired_services, fake_stub, stream.id, 110 * GB)
    s = await usage(wired_services, tenant)
    assert s.grace_remaining_bytes == 0 and s.blocked is True


# -- Auto top-up -----------------------------------------------------------------


@pytest.mark.asyncio
async def test_auto_top_up_enabled_buys_a_block_and_credits_on_payment(
    wired_services, tenant, fake_stub, stub_stripe_charge
):
    await put_on_plan(tenant, "builder", auto_top_up=True)  # 500 GB
    stream = await open_stream(wired_services, tenant, fake_stub)

    await meter(wired_services, fake_stub, stream.id, 500 * GB)

    # A $5 / 100 GB block was charged -- but nothing is credited until Stripe
    # says the invoice is paid.
    assert len(stub_stripe_charge) == 1
    s = await usage(wired_services, tenant)
    assert s.top_up_spent_cents == 500
    assert s.allowance_bytes == 500 * GB and s.grace_active is True

    top_up_id = stub_stripe_charge[0]
    paid = stripe_event(
        f"evt_paid_{uuid.uuid4().hex[:8]}",
        "invoice.paid",
        {
            "id": f"in_topup_{top_up_id}",
            "customer": tenant.stripe_customer_id,
            "status": "paid",
            "amount_due": 500,
            "amount_paid": 500,
            "metadata": {"kind": "data_top_up", "top_up_id": top_up_id},
        },
    )
    await wired_services.get_stripe_webhook_service().handle_event(paid)

    s = await usage(wired_services, tenant)
    assert s.allowance_bytes == 600 * GB
    assert s.remaining_bytes == 100 * GB
    assert s.blocked is False and s.grace_active is False
    assert await stream_state(wired_services, stream.id) == StreamState.ACTIVE

    # Stripe redelivers the same event: must not credit twice.
    await wired_services.get_stripe_webhook_service().handle_event(paid)
    assert (await usage(wired_services, tenant)).allowance_bytes == 600 * GB

    top_up = await wired_services.get_top_up_repo().get(top_up_id)
    assert top_up.status == TopUpStatus.PAID


@pytest.mark.asyncio
async def test_auto_top_up_is_not_double_charged_while_one_is_pending(
    wired_services, tenant, fake_stub, stub_stripe_charge
):
    await put_on_plan(tenant, "builder", auto_top_up=True)
    stream = await open_stream(wired_services, tenant, fake_stub)

    await meter(wired_services, fake_stub, stream.id, 500 * GB)
    await meter(wired_services, fake_stub, stream.id, 501 * GB)
    await meter(wired_services, fake_stub, stream.id, 502 * GB)

    assert len(stub_stripe_charge) == 1


@pytest.mark.asyncio
async def test_auto_top_up_disabled_never_charges(
    wired_services, tenant, fake_stub, stub_stripe_charge
):
    await put_on_plan(tenant, "builder", auto_top_up=False)
    stream = await open_stream(wired_services, tenant, fake_stub)

    await meter(wired_services, fake_stub, stream.id, 510 * GB)  # past grace (10 GB)

    assert stub_stripe_charge == []
    assert (await usage(wired_services, tenant)).blocked is True


@pytest.mark.asyncio
async def test_monthly_spending_cap_stops_auto_purchases_but_grace_still_applies(
    wired_services, tenant, fake_stub, stub_stripe_charge
):
    # Cap of $5 allows exactly one automatic $5 block per period.
    await put_on_plan(tenant, "builder", auto_top_up=True, cap_cents=500)
    stream = await open_stream(wired_services, tenant, fake_stub)

    await meter(wired_services, fake_stub, stream.id, 500 * GB)  # 1st top-up
    assert len(stub_stripe_charge) == 1
    top_up_id = stub_stripe_charge[0]
    await wired_services.get_stripe_webhook_service().handle_event(
        stripe_event(
            f"evt_paid_{uuid.uuid4().hex[:8]}",
            "invoice.paid",
            {
                "id": f"in_topup_{top_up_id}",
                "customer": tenant.stripe_customer_id,
                "metadata": {"kind": "data_top_up", "top_up_id": top_up_id},
            },
        )
    )

    # Burn through the purchased block; the next exhaustion would need a 2nd
    # automatic purchase, which the cap forbids.
    await meter(wired_services, fake_stub, stream.id, 600 * GB)

    assert len(stub_stripe_charge) == 1
    s = await usage(wired_services, tenant)
    assert UsageAlertKind.TOP_UP_CAP_REACHED.value in kinds(s)
    assert s.grace_active is True  # same limited grace policy as everyone
    await meter(wired_services, fake_stub, stream.id, 611 * GB)  # past grace
    assert (await usage(wired_services, tenant)).blocked is True


@pytest.mark.asyncio
async def test_enabling_auto_top_up_while_out_of_data_buys_immediately(
    wired_services, tenant, fake_stub, stub_stripe_charge
):
    await put_on_plan(tenant, "builder", auto_top_up=False)
    stream = await open_stream(wired_services, tenant, fake_stub)
    await meter(wired_services, fake_stub, stream.id, 500 * GB)
    assert stub_stripe_charge == []

    from cloud_api.models.api import InternalPatchBillingAccountRequest

    await wired_services.get_billing_account_repo().patch_billing_account(
        tenant.billing_account_id,
        InternalPatchBillingAccountRequest(auto_top_up_enabled=True),
    )
    await wired_services.get_usage_enforcement_service().on_auto_top_up_enabled(
        tenant.billing_account_id
    )

    assert len(stub_stripe_charge) == 1


@pytest.mark.asyncio
async def test_failed_top_up_payment_raises_alert_and_does_not_credit(
    wired_services, tenant, fake_stub, stub_stripe_charge
):
    await put_on_plan(tenant, "builder", auto_top_up=True)
    stream = await open_stream(wired_services, tenant, fake_stub)
    await meter(wired_services, fake_stub, stream.id, 500 * GB)
    top_up_id = stub_stripe_charge[0]

    await wired_services.get_stripe_webhook_service().handle_event(
        stripe_event(
            f"evt_fail_{uuid.uuid4().hex[:8]}",
            "invoice.payment_failed",
            {
                "id": f"in_topup_{top_up_id}",
                "customer": tenant.stripe_customer_id,
                "metadata": {"kind": "data_top_up", "top_up_id": top_up_id},
            },
        )
    )

    s = await usage(wired_services, tenant)
    assert s.allowance_bytes == 500 * GB
    assert UsageAlertKind.TOP_UP_FAILED.value in kinds(s)
    assert (await wired_services.get_top_up_repo().get(top_up_id)).status == (
        TopUpStatus.FAILED
    )


# -- Renewal / plan changes --------------------------------------------------------


def renewal_event(tenant, period_start: int, period_end: int, event_id=None):
    return stripe_event(
        event_id or f"evt_renew_{uuid.uuid4().hex[:8]}",
        "invoice.paid",
        {
            "id": f"in_{uuid.uuid4().hex[:8]}",
            "customer": tenant.stripe_customer_id,
            "subscription": tenant.stripe_subscription_id,
            "billing_reason": "subscription_cycle",
            "status": "paid",
            "lines": {"data": [{"period": {"start": period_start, "end": period_end}}]},
        },
    )


@pytest.mark.asyncio
async def test_paid_renewal_refills_the_allowance_and_rearms_alerts(
    wired_services, tenant, fake_stub
):
    await put_on_plan(tenant, "maker")
    stream = await open_stream(wired_services, tenant, fake_stub)
    await meter(wired_services, fake_stub, stream.id, 101 * GB)  # into grace
    before = await usage(wired_services, tenant)
    assert before.grace_active and "usage_100" in kinds(before)

    await wired_services.get_stripe_webhook_service().handle_event(
        renewal_event(tenant, 1_800_000_000, 1_802_592_000)
    )

    after = await usage(wired_services, tenant)
    assert after.remaining_bytes == 100 * GB  # the bug: this used to stay 0 forever
    assert after.grace_remaining_bytes == 0 and after.blocked is False
    assert kinds(after) == set()  # new period, fresh alerts


@pytest.mark.asyncio
async def test_replayed_renewal_does_not_refill_twice(
    wired_services, tenant, fake_stub
):
    await put_on_plan(tenant, "maker")
    stream = await open_stream(wired_services, tenant, fake_stub)
    await wired_services.get_stripe_webhook_service().handle_event(
        renewal_event(tenant, 1_800_000_000, 1_802_592_000)
    )
    await meter(wired_services, fake_stub, stream.id, 10 * GB)

    # Same billing period redelivered under a different Stripe event id.
    await wired_services.get_stripe_webhook_service().handle_event(
        renewal_event(tenant, 1_800_000_000, 1_802_592_000)
    )

    assert (await usage(wired_services, tenant)).used_bytes == 10 * GB


@pytest.mark.asyncio
async def test_failed_payment_does_not_refill(wired_services, tenant, fake_stub):
    await put_on_plan(tenant, "maker")
    stream = await open_stream(wired_services, tenant, fake_stub)
    await meter(wired_services, fake_stub, stream.id, 50 * GB)

    await wired_services.get_stripe_webhook_service().handle_event(
        stripe_event(
            f"evt_failed_{uuid.uuid4().hex[:8]}",
            "invoice.payment_failed",
            {
                "id": "in_failed",
                "customer": tenant.stripe_customer_id,
                "subscription": tenant.stripe_subscription_id,
                "billing_reason": "subscription_cycle",
            },
        )
    )

    assert (await usage(wired_services, tenant)).used_bytes == 50 * GB


@pytest.mark.asyncio
async def test_mid_period_upgrade_preserves_usage(
    wired_services, tenant, fake_stub, upgrade_target
):
    await put_on_plan(tenant, "maker")  # 100 GB
    stream = await open_stream(wired_services, tenant, fake_stub)
    await meter(wired_services, fake_stub, stream.id, 60 * GB)

    await wired_services.get_stripe_webhook_service().handle_event(
        stripe_event(
            f"evt_upd_{uuid.uuid4().hex[:8]}",
            "customer.subscription.updated",
            {
                "id": tenant.stripe_subscription_id,
                "status": "active",
                "items": {"data": [{"price": {"id": upgrade_target}}]},
            },
        )
    )

    s = await usage(wired_services, tenant)
    assert s.allowance_bytes == 500 * GB
    assert s.used_bytes == 60 * GB  # upgrading is not a usage reset
    assert s.remaining_bytes == 440 * GB


@pytest.mark.asyncio
async def test_cancellation_returns_to_free_with_hard_stop_policy(
    wired_services, tenant, fake_stub
):
    await put_on_plan(tenant, "builder", auto_top_up=True)

    await wired_services.get_stripe_webhook_service().handle_event(
        stripe_event(
            f"evt_del_{uuid.uuid4().hex[:8]}",
            "customer.subscription.deleted",
            {"id": tenant.stripe_subscription_id},
        )
    )

    s = await usage(wired_services, tenant)
    assert s.paid is False and s.allowance_bytes == 5 * GB
    assert s.grace_policy_bytes == 0 and s.can_top_up is False


# -- Webhook retry semantics -----------------------------------------------------


@pytest.mark.asyncio
async def test_failed_webhook_is_retried_not_swallowed_as_duplicate(
    wired_services, tenant, monkeypatch
):
    await put_on_plan(tenant, "maker")
    svc = wired_services.get_stripe_webhook_service()
    event = renewal_event(
        tenant,
        1_800_000_000,
        1_802_592_000,
        event_id=f"evt_flaky_{uuid.uuid4().hex[:8]}",
    )

    original = svc._renew_allowance
    calls = {"n": 0}

    async def flaky(invoice):
        calls["n"] += 1
        if calls["n"] == 1:
            raise RuntimeError("transient failure")
        await original(invoice)

    monkeypatch.setattr(svc, "_renew_allowance", flaky)

    with pytest.raises(RuntimeError):
        await svc.handle_event(event)
    await svc.handle_event(event)  # Stripe's retry of the same event id

    assert calls["n"] == 2  # second delivery was processed, not skipped
    assert (await usage(wired_services, tenant)).remaining_bytes == 100 * GB


# -- Connection slots ---------------------------------------------------------------


@pytest.mark.asyncio
async def test_slot_is_released_when_connection_creation_fails(
    wired_services, tenant, fake_stub, monkeypatch
):
    async def boom(req):
        raise RuntimeError("conn_worker down")

    monkeypatch.setattr(fake_stub, "CreateConnection", boom)
    svc = wired_services.get_connection_service()

    with pytest.raises(RuntimeError):
        await svc.create_connection(
            CreateConnectionRequest(name="c", node_id=tenant.node_id)
        )

    assert (await usage(wired_services, tenant)).connection_slots_remaining == 1


@pytest.mark.asyncio
async def test_leaked_slot_heals_when_worker_dropped_the_connection(
    wired_services, tenant, fake_stub
):
    svc = wired_services.get_connection_service()
    first = await svc.create_connection(
        CreateConnectionRequest(name="a", node_id=tenant.node_id)
    )
    assert (await usage(wired_services, tenant)).connection_slots_remaining == 0

    # The tunnel drops: conn_worker forgets the connection without cloud_api
    # ever calling terminate (so nothing releases the slot).
    fake_stub.connections.pop(first.id)
    svc._sync_connections.cache_clear()

    second = await svc.create_connection(
        CreateConnectionRequest(name="b", node_id=tenant.node_id)
    )

    assert second.id != first.id
    closed = await svc.conn_repo.get_connection(first.id)
    assert closed.state.value == "closed"
