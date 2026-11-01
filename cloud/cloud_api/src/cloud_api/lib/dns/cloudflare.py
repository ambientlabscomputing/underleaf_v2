import httpx

API_BASE = "https://api.cloudflare.com/client/v4"


class CloudflareDNSManager:
    """A records in one Cloudflare zone, DNS-only (not proxied): agents speak
    raw TCP/mTLS to the VM, which Cloudflare's proxy can't carry.

    The token needs `Zone:DNS:Edit` on the zone only.
    """

    def __init__(
        self, api_token: str, zone_id: str, client: httpx.AsyncClient | None = None
    ):
        self._zone = zone_id
        self._client = client or httpx.AsyncClient(
            base_url=API_BASE,
            headers={"Authorization": f"Bearer {api_token}"},
            timeout=30.0,
        )

    @staticmethod
    def _result(resp: httpx.Response):
        resp.raise_for_status()
        body = resp.json()
        if not body.get("success", False):
            raise RuntimeError(f"Cloudflare API error: {body.get('errors')}")
        return body.get("result")

    async def _find(self, name: str) -> list[dict]:
        resp = await self._client.get(
            f"/zones/{self._zone}/dns_records", params={"type": "A", "name": name}
        )
        return self._result(resp) or []

    async def upsert_a(self, name: str, ip: str, ttl: int = 60) -> None:
        record = {
            "type": "A",
            "name": name,
            "content": ip,
            "ttl": ttl,
            "proxied": False,
        }
        existing = await self._find(name)
        if existing:
            current = existing[0]
            if (
                current.get("content") == ip
                and current.get("ttl") == ttl
                and not current.get("proxied")
            ):
                return  # already right; spare the write
            resp = await self._client.put(
                f"/zones/{self._zone}/dns_records/{current['id']}", json=record
            )
        else:
            resp = await self._client.post(
                f"/zones/{self._zone}/dns_records", json=record
            )
        self._result(resp)

    async def delete_a(self, name: str, ip: str | None = None) -> None:
        for record in await self._find(name):
            if ip is not None and record.get("content") != ip:
                continue
            resp = await self._client.delete(
                f"/zones/{self._zone}/dns_records/{record['id']}"
            )
            if resp.status_code != 404:  # already gone
                self._result(resp)
