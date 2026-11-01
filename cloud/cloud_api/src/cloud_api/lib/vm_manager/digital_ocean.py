from datetime import datetime

import httpx

from cloud_api.lib.vm_manager.models import VM, VMProvider, VMSpec, VMStatus
from cloud_api.lib.vm_manager.protocol import VMManager

API_BASE = "https://api.digitalocean.com/v2"

# (cpus, mem_gb, slug), smallest first. A spec maps to the first slug that
# covers both; storage rides along with the slug (DO sizes disk by plan).
_SIZES = [
    (1, 1, "s-1vcpu-1gb"),
    (1, 2, "s-1vcpu-2gb"),
    (2, 2, "s-2vcpu-2gb"),
    (2, 4, "s-2vcpu-4gb"),
    (4, 8, "s-4vcpu-8gb"),
    (8, 16, "s-8vcpu-16gb"),
]
_SLUG_SPECS = {slug: (c, m) for c, m, slug in _SIZES}


def size_slug(spec: VMSpec) -> str:
    for cpus, mem_gb, slug in _SIZES:
        if cpus >= spec.cpus and mem_gb >= spec.mem_gb:
            return slug
    raise ValueError(f"no DigitalOcean size covers {spec.cpus} CPU / {spec.mem_gb} GB")


def _tag(key: str, value: str) -> str:
    return f"{key}:{value}"


def _parse_tags(tags: list[str]) -> dict[str, str]:
    return dict(t.split(":", 1) for t in tags if ":" in t)


def _droplet_name(hostname: str) -> str:
    # DO droplet names must be valid hostnames: no underscores.
    return hostname.replace("_", "-").lower()


class DOVMManager(VMManager):
    """DigitalOcean droplets over the REST API.

    Tags are stored as "key:value" strings (DO tags are flat), so `list_vms`
    can filter on the dict form the rest of the system uses.
    """

    def __init__(
        self,
        token: str,
        region: str = "nyc3",
        image: str = "ubuntu-24-04-x64",
        ssh_key_ids: list[str] | None = None,
        client: httpx.AsyncClient | None = None,
    ):
        self._region = region
        self._image = image
        self._ssh_keys = ssh_key_ids or []
        self._client = client or httpx.AsyncClient(
            base_url=API_BASE,
            headers={"Authorization": f"Bearer {token}"},
            timeout=30.0,
        )
        self._has_token = bool(token) or client is not None

    def _require_token(self) -> None:
        if not self._has_token:
            raise RuntimeError(
                "DigitalOcean token not configured "
                "(worker_defaults.digital_ocean.token or UNDERLEAF_DO_TOKEN)"
            )

    @staticmethod
    def _to_vm(d: dict) -> VM:
        public = private = None
        for net in d.get("networks", {}).get("v4", []):
            if net.get("type") == "public":
                public = net["ip_address"]
            elif net.get("type") == "private":
                private = net["ip_address"]
        do_status = d.get("status")
        if do_status == "active" and public:
            status = VMStatus.ACTIVE
        elif do_status in ("new", "active"):
            status = VMStatus.PENDING
        else:
            status = VMStatus.ERROR
        cpus, mem_gb = _SLUG_SPECS.get(
            d.get("size_slug", ""), (d.get("vcpus", 0), d.get("memory", 0) // 1024)
        )
        created = d.get("created_at")
        return VM(
            spec=VMSpec(cpus=cpus, mem_gb=mem_gb, storage_gb=d.get("disk", 0)),
            provider_id=str(d["id"]),
            hostname=d["name"],
            os=(d.get("image") or {}).get("distribution", ""),
            status=status,
            provider=VMProvider.DIGITAL_OCEAN,
            public_ip=public,
            private_ip=private,
            tags=_parse_tags(d.get("tags", [])),
            created_at=datetime.fromisoformat(created) if created else None,
        )

    async def create_vm(
        self,
        hostname: str,
        spec: VMSpec,
        user_data: str = "",
        tags: dict[str, str] | None = None,
    ) -> VM:
        self._require_token()
        body: dict = {
            "name": _droplet_name(hostname),
            "region": spec.region or self._region,
            "size": size_slug(spec),
            "image": self._image,
            "user_data": user_data,
            "tags": [_tag(k, v) for k, v in (tags or {}).items()],
            "monitoring": True,
        }
        if self._ssh_keys:
            body["ssh_keys"] = self._ssh_keys
        resp = await self._client.post("/droplets", json=body)
        resp.raise_for_status()
        return self._to_vm(resp.json()["droplet"])

    async def get_vm(self, provider_id: str) -> VM | None:
        self._require_token()
        resp = await self._client.get(f"/droplets/{provider_id}")
        if resp.status_code == 404:
            return None
        resp.raise_for_status()
        return self._to_vm(resp.json()["droplet"])

    async def delete_vm(self, provider_id: str) -> None:
        self._require_token()
        resp = await self._client.delete(f"/droplets/{provider_id}")
        if resp.status_code not in (204, 404):  # 404: already gone -- idempotent
            resp.raise_for_status()

    async def list_vms(self, tags: dict[str, str] | None = None) -> list[VM]:
        self._require_token()
        wanted = list((tags or {}).items())
        params: dict = {"per_page": 200}
        if wanted:  # DO filters by one tag server-side; the rest are checked here
            params["tag_name"] = _tag(*wanted[0])
        vms: list[VM] = []
        page = 1
        while True:
            resp = await self._client.get("/droplets", params={**params, "page": page})
            resp.raise_for_status()
            body = resp.json()
            vms += [self._to_vm(d) for d in body.get("droplets", [])]
            if not body.get("links", {}).get("pages", {}).get("next"):
                break
            page += 1
        return [vm for vm in vms if dict(wanted).items() <= vm.tags.items()]

    async def run_cmd(self, vm_hostname: str, *cmd: str, cwd: str = ".") -> None:
        raise NotImplementedError(
            "SSH bootstrap is not implemented; use the cloud-init bootstrap"
        )
