"""Unit tests (mocked repos) for EntitlementsBucketService decision logic.

Atomicity and the actual SQL are proven against Postgres in
tests/integration; these pin the branching around them.
"""

from datetime import UTC, datetime, timedelta
from unittest.mock import AsyncMock

import pytest

from cloud_api.models.api import EntitlementsBucket, UsageAlertKind
from cloud_api.repository.entitlements_bucket_repository import (
    EntitlementsBalanceField,
)
from cloud_api.service.entitlements_bucket_service import (
    ChargeOutcome,
    EntitlementsBucketService,
    EntitlementsExceededError,
    percent_used,
)

GB = 10**9


def _bucket(**overrides) -> EntitlementsBucket:
    defaults = {
        "id": "entitlements_1",
        "billing_account_id": "billing_1",
        "name": "b",
        "network_traffic_allowance": 100 * GB,
        "network_traffic_balance": 100 * GB,
        "connection_slot_limit": 1,
        "connection_slot_balance": 1,
        "grace_policy_bytes": 2 * GB,
        "stripe_managed_period": True,
        "period_start": datetime.now(UTC) - timedelta(days=1),
        "period_end": datetime.now(UTC) + timedelta(days=29),
    }
    defaults.update(overrides)
    return EntitlementsBucket(**defaults)


def _service(bucket=None):
    repo, alerts = AsyncMock(), AsyncMock()
    repo.get_entitlements_bucket_by_billing_account.return_value = bucket
    alerts.record_if_new.return_value = True
    return EntitlementsBucketService(repo, alerts), repo, alerts


# -- connection slots ------------------------------------------------------------


@pytest.mark.asyncio
async def test_slot_reservation_fails_open_without_a_bucket():
    svc, repo, _ = _service(None)
    await svc.check_and_reserve_connection_slot("billing_1")
    repo.decrement_balance.assert_not_awaited()


@pytest.mark.asyncio
async def test_slot_reservation_decrements_atomically():
    svc, repo, _ = _service(_bucket())
    repo.decrement_balance.return_value = _bucket(connection_slot_balance=0)
    await svc.check_and_reserve_connection_slot("billing_1")
    repo.decrement_balance.assert_awaited_once_with(
        "entitlements_1", EntitlementsBalanceField.CONNECTION_SLOT, 1
    )


@pytest.mark.asyncio
async def test_slot_reservation_fails_closed_when_full():
    svc, repo, _ = _service(_bucket(connection_slot_balance=0))
    repo.decrement_balance.return_value = None
    with pytest.raises(EntitlementsExceededError):
        await svc.check_and_reserve_connection_slot("billing_1")


@pytest.mark.asyncio
async def test_release_increments_slot_and_ignores_missing_bucket():
    svc, repo, _ = _service(_bucket())
    await svc.release_connection_slot("billing_1")
    repo.increment_balance.assert_awaited_once_with(
        "entitlements_1", EntitlementsBalanceField.CONNECTION_SLOT, 1
    )
    svc, repo, _ = _service(None)
    await svc.release_connection_slot("billing_1")
    repo.increment_balance.assert_not_awaited()


@pytest.mark.asyncio
async def test_reconcile_sets_slots_from_open_connection_count():
    svc, repo, _ = _service(_bucket())
    await svc.reconcile_connection_slots("billing_1", 3)
    repo.set_slot_balance.assert_awaited_once_with("entitlements_1", 3)


# -- traffic gate ----------------------------------------------------------------


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "bucket,expected",
    [
        (None, True),  # fail open
        (_bucket(network_traffic_balance=1), True),
        (_bucket(network_traffic_balance=0, grace_balance=1), True),  # on grace
        (_bucket(network_traffic_balance=0, grace_balance=0), False),
    ],
)
async def test_new_stream_gate(bucket, expected):
    svc, _, _ = _service(bucket)
    assert await svc.has_network_traffic_balance("billing_1") is expected


# -- charging --------------------------------------------------------------------


@pytest.mark.asyncio
async def test_charge_is_a_noop_for_zero_bytes_or_missing_bucket():
    svc, repo, _ = _service(_bucket())
    assert await svc.charge_network_traffic("billing_1", 0) is None
    svc, repo, _ = _service(None)
    assert await svc.charge_network_traffic("billing_1", 5) is None
    repo.draw_traffic.assert_not_awaited()


@pytest.mark.asyncio
async def test_paid_plan_crossing_thresholds_records_each_alert():
    svc, repo, alerts = _service(_bucket())
    repo.draw_traffic.return_value = _bucket(network_traffic_balance=4 * GB)  # 96%

    outcome = await svc.charge_network_traffic("billing_1", 96 * GB)

    assert outcome.new_alerts == [UsageAlertKind.USAGE_80, UsageAlertKind.USAGE_95]
    assert alerts.record_if_new.await_count == 2


@pytest.mark.asyncio
async def test_free_plan_only_gets_the_100_percent_notice():
    free = _bucket(stripe_managed_period=False, grace_policy_bytes=0)
    svc, repo, _ = _service(free)
    repo.draw_traffic.return_value = _bucket(
        stripe_managed_period=False,
        grace_policy_bytes=0,
        network_traffic_balance=0,
    )
    repo.grant_grace.return_value = None

    outcome = await svc.charge_network_traffic("billing_1", 100 * GB)

    assert outcome.new_alerts == [UsageAlertKind.USAGE_100]
    assert outcome.hard_stop is True


@pytest.mark.asyncio
async def test_reaching_zero_grants_grace_and_charges_the_overshoot_to_it():
    svc, repo, _ = _service(_bucket(network_traffic_balance=10 * GB))
    repo.draw_traffic.side_effect = [
        _bucket(network_traffic_balance=0),  # drew the 10 GB that was left
        _bucket(network_traffic_balance=0, grace_balance=GB),  # overshoot -> grace
    ]
    repo.grant_grace.return_value = _bucket(
        network_traffic_balance=0,
        grace_balance=2 * GB,
        grace_granted_at=datetime.now(UTC),
    )

    outcome = await svc.charge_network_traffic("billing_1", 11 * GB)

    assert outcome.grace_granted is True
    assert repo.draw_traffic.await_args_list[1].args == ("entitlements_1", 1 * GB)
    assert outcome.bucket.grace_balance == GB
    assert outcome.hard_stop is False


# -- ChargeOutcome.hard_stop / percent_used -------------------------------------------


def test_hard_stop_only_when_balance_and_grace_are_gone():
    now = datetime.now(UTC)
    assert ChargeOutcome(_bucket(network_traffic_balance=1)).hard_stop is False
    assert (
        ChargeOutcome(_bucket(network_traffic_balance=0, grace_balance=1)).hard_stop
        is False
    )
    # grace not yet granted: it is about to be, so not a stop
    assert ChargeOutcome(_bucket(network_traffic_balance=0)).hard_stop is False
    spent = _bucket(network_traffic_balance=0, grace_balance=0, grace_granted_at=now)
    assert ChargeOutcome(spent).hard_stop is True
    free = _bucket(network_traffic_balance=0, grace_policy_bytes=0)
    assert ChargeOutcome(free).hard_stop is True


def test_percent_used():
    assert percent_used(_bucket(network_traffic_balance=20 * GB)) == 80.0
    assert (
        percent_used(_bucket(network_traffic_allowance=0, network_traffic_balance=0))
        == 0.0
    )


# -- free period rollover --------------------------------------------------------------


@pytest.mark.asyncio
async def test_free_bucket_refills_when_its_month_has_ended():
    stale = _bucket(
        stripe_managed_period=False,
        network_traffic_balance=0,
        period_end=datetime.now(UTC) - timedelta(days=1),
    )
    svc, repo, _ = _service(stale)
    repo.apply_plan.return_value = _bucket(stripe_managed_period=False)

    await svc.get_entitlements_bucket_by_billing_account("billing_1")

    repo.apply_plan.assert_awaited_once()
    assert repo.apply_plan.await_args.kwargs["reset_usage"] is True
    assert repo.apply_plan.await_args.kwargs["stripe_managed"] is False


@pytest.mark.asyncio
async def test_paid_bucket_is_never_refilled_by_the_clock():
    expired_paid = _bucket(
        stripe_managed_period=True, period_end=datetime.now(UTC) - timedelta(days=5)
    )
    svc, repo, _ = _service(expired_paid)

    await svc.get_entitlements_bucket_by_billing_account("billing_1")

    repo.apply_plan.assert_not_awaited()
