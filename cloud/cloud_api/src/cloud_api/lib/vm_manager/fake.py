from datetime import UTC, datetime
from itertools import count

from cloud_api.lib.vm_manager.models import VM, VMProvider, VMSpec, VMStatus
from cloud_api.lib.vm_manager.protocol import VMManager


class FakeVMManager(VMManager):
    """In-memory provider for tests and for running the API locally without
    cloud spend. VMs are ACTIVE immediately; knobs let tests simulate failures.

    The shared adapter-conformance tests run against this and, later, against
    each real adapter.
    """

    def __init__(self) -> None:
        self.vms: dict[str, VM] = {}
        self.user_data: dict[str, str] = {}
        self.commands: list[tuple[str, tuple[str, ...], str]] = []
        self.fail_next_create: Exception | None = None
        self.create_status = VMStatus.ACTIVE  # set PENDING to simulate a slow boot
        self._ids = count(1)

    async def create_vm(
        self,
        hostname: str,
        spec: VMSpec,
        user_data: str = "",
        tags: dict[str, str] | None = None,
    ) -> VM:
        if self.fail_next_create is not None:
            err, self.fail_next_create = self.fail_next_create, None
            raise err
        n = next(self._ids)
        vm = VM(
            spec=spec,
            provider_id=f"fake-{n}",
            hostname=hostname,
            os="fake-os",
            status=self.create_status,
            provider=VMProvider.FAKE,
            public_ip=f"203.0.113.{n}"
            if self.create_status == VMStatus.ACTIVE
            else None,
            tags=dict(tags or {}),
            created_at=datetime.now(UTC),
        )
        self.vms[vm.provider_id] = vm
        self.user_data[vm.provider_id] = user_data
        return vm

    def activate(self, provider_id: str) -> None:
        """Finish a PENDING boot (test helper)."""
        vm = self.vms[provider_id]
        self.vms[provider_id] = vm.model_copy(
            update={
                "status": VMStatus.ACTIVE,
                "public_ip": f"203.0.113.{provider_id.removeprefix('fake-')}",
            }
        )

    async def get_vm(self, provider_id: str) -> VM | None:
        return self.vms.get(provider_id)

    async def delete_vm(self, provider_id: str) -> None:
        self.vms.pop(provider_id, None)
        self.user_data.pop(provider_id, None)

    async def list_vms(self, tags: dict[str, str] | None = None) -> list[VM]:
        wanted = (tags or {}).items()
        return [vm for vm in self.vms.values() if wanted <= vm.tags.items()]

    async def run_cmd(self, vm_hostname: str, *cmd: str, cwd: str = ".") -> None:
        self.commands.append((vm_hostname, cmd, cwd))
