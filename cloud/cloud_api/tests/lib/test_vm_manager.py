"""Adapter conformance: any VMManager must pass these. Real provider adapters
get parametrized in here once they exist (against a sandbox account)."""

import pytest

from cloud_api.lib.vm_manager import (
    FakeVMManager,
    VMProvider,
    VMSpec,
    VMStatus,
    vm_manager_factory,
)

SPEC = VMSpec(cpus=2, mem_gb=4, storage_gb=20)


@pytest.fixture(params=["fake"])
def vm_manager(request):
    return FakeVMManager()


@pytest.mark.asyncio
async def test_create_then_get(vm_manager):
    vm = await vm_manager.create_vm(
        "h1", SPEC, user_data="#cloud-config", tags={"a": "b"}
    )
    assert vm.hostname == "h1" and vm.spec == SPEC and vm.tags == {"a": "b"}
    assert (await vm_manager.get_vm(vm.provider_id)) is not None


@pytest.mark.asyncio
async def test_get_missing_returns_none(vm_manager):
    assert await vm_manager.get_vm("nope") is None


@pytest.mark.asyncio
async def test_delete_is_idempotent(vm_manager):
    vm = await vm_manager.create_vm("h1", SPEC)
    await vm_manager.delete_vm(vm.provider_id)
    await vm_manager.delete_vm(vm.provider_id)
    assert await vm_manager.get_vm(vm.provider_id) is None


@pytest.mark.asyncio
async def test_list_filters_by_all_tags(vm_manager):
    a = await vm_manager.create_vm("a", SPEC, tags={"app": "underleaf", "env": "prod"})
    await vm_manager.create_vm("b", SPEC, tags={"app": "underleaf", "env": "dev"})
    await vm_manager.create_vm("c", SPEC)
    assert len(await vm_manager.list_vms({"app": "underleaf"})) == 2
    assert [v.provider_id for v in await vm_manager.list_vms({"env": "prod"})] == [
        a.provider_id
    ]
    assert len(await vm_manager.list_vms()) == 3


@pytest.mark.asyncio
async def test_fake_pending_boot_then_activate():
    m = FakeVMManager()
    m.create_status = VMStatus.PENDING
    vm = await m.create_vm("h", SPEC)
    assert vm.status == VMStatus.PENDING and vm.public_ip is None
    m.activate(vm.provider_id)
    vm = await m.get_vm(vm.provider_id)
    assert vm.status == VMStatus.ACTIVE and vm.public_ip


@pytest.mark.asyncio
async def test_fake_injected_create_failure_is_one_shot():
    m = FakeVMManager()
    m.fail_next_create = RuntimeError("quota")
    with pytest.raises(RuntimeError):
        await m.create_vm("h", SPEC)
    assert (await m.create_vm("h", SPEC)).hostname == "h"


def test_factory_builds_fake():
    assert isinstance(vm_manager_factory(VMProvider.FAKE), FakeVMManager)
