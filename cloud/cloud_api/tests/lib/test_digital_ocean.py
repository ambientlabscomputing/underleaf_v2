"""DOVMManager against a mocked DO API (no network)."""

import json

import httpx
import pytest

from cloud_api.lib.vm_manager import VMSpec, VMStatus
from cloud_api.lib.vm_manager.digital_ocean import DOVMManager, size_slug

SPEC = VMSpec(cpus=2, mem_gb=4, storage_gb=20)


def droplet(id=1, status="active", ip="203.0.113.9", tags=None):
    return {
        "id": id,
        "name": f"underleaf-w-{id}",
        "status": status,
        "size_slug": "s-2vcpu-4gb",
        "disk": 80,
        "created_at": "2026-10-30T12:00:00Z",
        "image": {"distribution": "Ubuntu"},
        "networks": {"v4": [{"type": "public", "ip_address": ip}] if ip else []},
        "tags": tags or [],
    }


def manager(handler) -> DOVMManager:
    client = httpx.AsyncClient(
        base_url="https://do.test/v2", transport=httpx.MockTransport(handler)
    )
    return DOVMManager(token="t", client=client)


def test_size_slug_picks_smallest_covering():
    assert size_slug(VMSpec(cpus=2, mem_gb=4, storage_gb=1)) == "s-2vcpu-4gb"
    assert size_slug(VMSpec(cpus=1, mem_gb=1, storage_gb=1)) == "s-1vcpu-1gb"
    assert size_slug(VMSpec(cpus=3, mem_gb=2, storage_gb=1)) == "s-4vcpu-8gb"
    with pytest.raises(ValueError):
        size_slug(VMSpec(cpus=64, mem_gb=1, storage_gb=1))


@pytest.mark.asyncio
async def test_create_vm_sends_valid_name_tags_and_user_data():
    seen = {}

    def handler(req: httpx.Request) -> httpx.Response:
        seen.update(json.loads(req.content))
        return httpx.Response(
            202, json={"droplet": droplet(7, "new", None, ["worker_id:w1"])}
        )

    vm = await manager(handler).create_vm(
        "host_conn_worker_w1", SPEC, user_data="#cloud-config", tags={"worker_id": "w1"}
    )
    assert (
        seen["name"] == "host-conn-worker-w1"
    )  # underscores aren't valid in droplet names
    assert seen["size"] == "s-2vcpu-4gb" and seen["user_data"] == "#cloud-config"
    assert seen["tags"] == ["worker_id:w1"]
    assert (
        vm.provider_id == "7" and vm.status == VMStatus.PENDING and vm.public_ip is None
    )
    assert vm.tags == {"worker_id": "w1"}


@pytest.mark.asyncio
async def test_active_without_public_ip_is_still_pending():
    m = manager(
        lambda r: httpx.Response(
            200, json={"droplet": droplet(status="active", ip=None)}
        )
    )
    assert (await m.get_vm("1")).status == VMStatus.PENDING


@pytest.mark.asyncio
async def test_get_active_vm_maps_fields():
    m = manager(lambda r: httpx.Response(200, json={"droplet": droplet(3)}))
    vm = await m.get_vm("3")
    assert vm.status == VMStatus.ACTIVE and vm.public_ip == "203.0.113.9"
    assert vm.created_at is not None and vm.spec.cpus == 2


@pytest.mark.asyncio
async def test_get_missing_is_none_and_delete_is_idempotent():
    m = manager(lambda r: httpx.Response(404, json={}))
    assert await m.get_vm("nope") is None
    await m.delete_vm("nope")  # 404 == already gone


@pytest.mark.asyncio
async def test_delete_error_propagates():
    m = manager(lambda r: httpx.Response(500))
    with pytest.raises(httpx.HTTPStatusError):
        await m.delete_vm("1")


@pytest.mark.asyncio
async def test_list_filters_all_tags_and_paginates():
    calls = []

    def handler(req: httpx.Request) -> httpx.Response:
        calls.append(dict(req.url.params))
        page = int(req.url.params["page"])
        body = {
            "droplets": [
                droplet(
                    page,
                    tags=["app:underleaf", f"env:{'prod' if page == 1 else 'dev'}"],
                )
            ],
            "links": {"pages": {"next": "x"}} if page == 1 else {},
        }
        return httpx.Response(200, json=body)

    vms = await manager(handler).list_vms({"app": "underleaf", "env": "prod"})
    assert [v.provider_id for v in vms] == [
        "1"
    ]  # page 2 is env:dev, filtered out locally
    assert len(calls) == 2 and calls[0]["tag_name"] == "app:underleaf"


@pytest.mark.asyncio
async def test_missing_token_fails_loudly():
    with pytest.raises(RuntimeError, match="token"):
        await DOVMManager(token="").create_vm("h", SPEC)
