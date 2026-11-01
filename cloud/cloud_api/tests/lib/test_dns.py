"""CloudflareDNSManager against a mocked Cloudflare API (no network), plus the
fake used by the rest of the suite."""

import json

import httpx
import pytest

from cloud_api.config import WorkerDefaults
from cloud_api.lib.dns import CloudflareDNSManager, FakeDNSManager, dns_manager_factory

ZONE = "zone123"


def ok(result):
    return httpx.Response(200, json={"success": True, "errors": [], "result": result})


def manager(handler) -> CloudflareDNSManager:
    client = httpx.AsyncClient(
        base_url="https://cf.test/client/v4", transport=httpx.MockTransport(handler)
    )
    return CloudflareDNSManager("tok", ZONE, client=client)


def record(
    id="r1", name="tunnel.example.com", content="203.0.113.1", ttl=60, proxied=False
):
    return {"id": id, "name": name, "content": content, "ttl": ttl, "proxied": proxied}


@pytest.mark.asyncio
async def test_upsert_creates_a_dns_only_record_when_missing():
    seen = []

    def handler(req: httpx.Request):
        seen.append((req.method, req.url.path, req.url.params.get("name")))
        if req.method == "GET":
            return ok([])
        body = json.loads(req.content)
        assert body == {
            "type": "A",
            "name": "tunnel.example.com",
            "content": "203.0.113.9",
            "ttl": 60,
            "proxied": False,  # agents speak raw TCP; Cloudflare's proxy can't carry it
        }
        return ok(record())

    await manager(handler).upsert_a("tunnel.example.com", "203.0.113.9")

    assert seen[-1][0] == "POST"
    assert seen[-1][1] == f"/client/v4/zones/{ZONE}/dns_records"
    assert seen[0][2] == "tunnel.example.com"


@pytest.mark.asyncio
async def test_upsert_repoints_an_existing_record():
    calls = []

    def handler(req: httpx.Request):
        calls.append((req.method, req.url.path))
        if req.method == "GET":
            return ok([record(id="abc", content="198.51.100.7")])
        assert json.loads(req.content)["content"] == "203.0.113.9"
        return ok(record())

    await manager(handler).upsert_a("tunnel.example.com", "203.0.113.9")

    assert calls[-1] == ("PUT", f"/client/v4/zones/{ZONE}/dns_records/abc")


@pytest.mark.asyncio
async def test_upsert_is_a_no_op_when_already_correct():
    calls = []

    def handler(req: httpx.Request):
        calls.append(req.method)
        return ok([record(content="203.0.113.9")])

    await manager(handler).upsert_a("tunnel.example.com", "203.0.113.9")

    assert calls == ["GET"]


@pytest.mark.asyncio
async def test_delete_only_removes_a_record_still_pointing_at_that_ip():
    deleted = []

    def handler(req: httpx.Request):
        if req.method == "GET":
            return ok([record(id="new", content="203.0.113.50")])
        deleted.append(req.url.path)
        return ok({"id": "new"})

    mgr = manager(handler)
    await mgr.delete_a("tunnel.example.com", ip="203.0.113.9")  # a newer worker owns it
    assert deleted == []

    await mgr.delete_a("tunnel.example.com", ip="203.0.113.50")
    assert deleted == [f"/client/v4/zones/{ZONE}/dns_records/new"]


@pytest.mark.asyncio
async def test_delete_of_a_missing_record_is_fine():
    await manager(lambda req: ok([])).delete_a("gone.example.com")


@pytest.mark.asyncio
async def test_api_level_errors_are_raised():
    def handler(req: httpx.Request):
        return httpx.Response(
            200, json={"success": False, "errors": [{"code": 9109, "message": "nope"}]}
        )

    with pytest.raises(RuntimeError, match="9109"):
        await manager(handler).upsert_a("x.example.com", "203.0.113.9")


@pytest.mark.asyncio
async def test_http_errors_are_raised():
    with pytest.raises(httpx.HTTPStatusError):
        await manager(lambda req: httpx.Response(403)).upsert_a(
            "x.example.com", "203.0.113.9"
        )


@pytest.mark.asyncio
async def test_fake_delete_respects_ip():
    fake = FakeDNSManager()
    await fake.upsert_a("a.example.com", "203.0.113.1")
    await fake.delete_a("a.example.com", ip="203.0.113.2")
    assert fake.records == {"a.example.com": "203.0.113.1"}
    await fake.delete_a("a.example.com", ip="203.0.113.1")
    assert fake.records == {}


def test_factory_needs_credentials():
    assert dns_manager_factory(WorkerDefaults()) is None
    configured = WorkerDefaults(cloudflare={"api_token": "t", "zone_id": "z"})
    assert isinstance(dns_manager_factory(configured), CloudflareDNSManager)
