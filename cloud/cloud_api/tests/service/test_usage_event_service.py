from datetime import UTC, datetime
from unittest.mock import AsyncMock

import pytest

from cloud_api.models.api import (
    CreateUsageEventRequest,
    UsageEvent,
    UsageEventType,
    UsageUnits,
)
from cloud_api.service.usage_event_service import (
    UsageEventNotFoundError,
    UsageEventService,
)


def _event(**overrides) -> UsageEvent:
    now = datetime.now(UTC)
    defaults = {
        "id": "usage_1",
        "billing_account_id": "billing_1",
        "event_type": UsageEventType.DATA_USAGE,
        "usage_unit": UsageUnits.BYTES,
        "usage_amount": 1024,
        "period_start": now,
        "period_end": now,
        "idempotency_key": "stream_1:0:1024",
    }
    defaults.update(overrides)
    return UsageEvent(**defaults)


KWARGS = {
    "billing_account_id": "billing_1",
    "event_type": UsageEventType.DATA_USAGE,
    "usage_unit": UsageUnits.BYTES,
    "usage_amount": 1024,
    "period_start": datetime.now(UTC),
    "period_end": datetime.now(UTC),
    "idempotency_key": "stream_1:0:1024",
    "stream_id": "stream_1",
}


@pytest.mark.asyncio
async def test_record_usage_builds_request_and_reports_created():
    repo = AsyncMock()
    repo.create_usage_event_idempotent.return_value = (_event(), True)

    event, created = await UsageEventService(repo).record_usage(**KWARGS)

    assert created is True and event.id == "usage_1"
    (sent,) = repo.create_usage_event_idempotent.await_args.args
    assert isinstance(sent, CreateUsageEventRequest)
    assert sent.idempotency_key == "stream_1:0:1024"
    assert sent.stream_id == "stream_1"


@pytest.mark.asyncio
async def test_record_usage_replay_reports_not_created_so_callers_skip_charging():
    repo = AsyncMock()
    repo.create_usage_event_idempotent.return_value = (_event(), False)

    _, created = await UsageEventService(repo).record_usage(**KWARGS)

    assert created is False


@pytest.mark.asyncio
async def test_get_and_delete_unknown_event_raise():
    repo = AsyncMock()
    repo.get_usage_event.return_value = None
    repo.delete_usage_event.return_value = False
    svc = UsageEventService(repo)
    with pytest.raises(UsageEventNotFoundError):
        await svc.get_usage_event("x")
    with pytest.raises(UsageEventNotFoundError):
        await svc.delete_usage_event("x")
