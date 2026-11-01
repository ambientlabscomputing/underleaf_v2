"""WorkerRepository against real Postgres (skipped when the migrated schema
isn't available): persistence round-trip, the one-live-worker-per-type
guard, and listing/filtering."""

import asyncio

import pytest
import pytest_asyncio
from sqlalchemy import text

from cloud_api.models.api.common import Status
from cloud_api.models.api.worker import (
    QueryWorkerRequest,
    Worker,
    WorkerConfig,
    WorkerState,
    WorkerType,
)
from cloud_api.models.base import IDPrefix, generate_id
from cloud_api.repository import session as db_session
from cloud_api.repository.worker_repository import WorkerRepository

pytestmark = pytest.mark.asyncio

CONFIG = WorkerConfig(
    cpu=2, mem_gb=4, storage_gb=20, repository="r", install_cmd=["i"], init_cmd=["x"]
)


def _worker() -> Worker:
    return Worker(
        id=generate_id(IDPrefix.WORKER),
        worker_type=WorkerType.CONNECTION_WORKER,
        config=CONFIG,
        state=WorkerState.REQUESTED,
        status=Status.IN_PROGRESS,
    )


@pytest_asyncio.fixture
async def repo(db):
    async with db_session.get_session() as s:
        try:
            await s.execute(text("DELETE FROM underleaf.workers"))
            await s.commit()
        except Exception as exc:  # noqa: BLE001
            pytest.skip(f"workers table not migrated: {exc}")
    return WorkerRepository()


async def test_create_and_get_round_trip(repo):
    w = await repo.create_worker(_worker())
    got = await repo.get_worker(w.id)
    assert got is not None
    assert got.config == CONFIG
    assert (got.state, got.status) == (WorkerState.REQUESTED, Status.IN_PROGRESS)
    assert got.state_changed_at is not None


async def test_second_live_worker_of_same_type_is_returned_not_created(repo):
    first, created1 = await repo.get_or_create_live_worker(_worker())
    second, created2 = await repo.get_or_create_live_worker(_worker())
    assert created1 and not created2
    assert second.id == first.id


async def test_concurrent_requests_create_exactly_one_worker(repo):
    results = await asyncio.gather(
        *(repo.get_or_create_live_worker(_worker()) for _ in range(8))
    )
    assert sum(created for _, created in results) == 1
    assert len({w.id for w, _ in results}) == 1


async def test_deleted_worker_frees_the_slot(repo):
    first, _ = await repo.get_or_create_live_worker(_worker())
    await repo.update_worker(
        first.id, state=WorkerState.DELETED, status=Status.SUCCEEDED
    )
    second, created = await repo.get_or_create_live_worker(_worker())
    assert created and second.id != first.id
    assert (await repo.get_live_worker(WorkerType.CONNECTION_WORKER)).id == second.id


async def test_teardown_in_progress_still_holds_the_slot(repo):
    first, _ = await repo.get_or_create_live_worker(_worker())
    await repo.update_worker(
        first.id, state=WorkerState.DELETED, status=Status.IN_PROGRESS
    )
    # DELETED + IN_PROGRESS: the VM may still exist, so no second worker yet.
    again, created = await repo.get_or_create_live_worker(_worker())
    assert not created and again.id == first.id
    assert again.is_live and not again.is_gone
    assert [w.id for w in await repo.list_live_workers()] == [first.id]

    await repo.update_worker(
        first.id, state=WorkerState.DELETED, status=Status.SUCCEEDED
    )
    assert (await repo.get_worker(first.id)).is_gone
    assert await repo.list_live_workers() == []


async def test_update_sets_state_changed_at_and_returns_worker(repo):
    w = await repo.create_worker(_worker())
    updated = await repo.update_worker(
        w.id, state=WorkerState.CREATED, status=Status.SUCCEEDED, extra_data={"a": 1}
    )
    assert updated.state == WorkerState.CREATED and updated.extra_data == {"a": 1}
    assert updated.state_changed_at >= w.state_changed_at
    assert await repo.update_worker("missing", state=WorkerState.CREATED) is None


async def test_list_filters_and_counts(repo):
    w = await repo.create_worker(_worker())
    await repo.update_worker(w.id, state=WorkerState.DELETED, status=Status.SUCCEEDED)
    await repo.create_worker(_worker())

    total, items = await repo.list_workers(
        QueryWorkerRequest(worker_type=WorkerType.CONNECTION_WORKER)
    )
    assert total == 2 and len(items) == 2

    total, items = await repo.list_workers(
        QueryWorkerRequest(state=WorkerState.DELETED)
    )
    assert total == 1 and items[0].id == w.id

    total, items = await repo.list_workers(QueryWorkerRequest(limit=1))
    assert total == 2 and len(items) == 1  # count ignores pagination
