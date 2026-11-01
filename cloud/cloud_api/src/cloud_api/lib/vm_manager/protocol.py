from typing import Protocol

from cloud_api.lib.vm_manager.models import VM, VMSpec


class VMManager(Protocol):
    """Provider adapter. Services only ever see these provider-neutral calls;
    sizes/images/firewalls/tag syntax stay inside the adapter, so supporting a
    new provider means implementing this and nothing else."""

    async def create_vm(
        self,
        hostname: str,
        spec: VMSpec,
        user_data: str = "",
        tags: dict[str, str] | None = None,
    ) -> VM:
        """Request a VM. `user_data` is a cloud-init payload run on first boot.
        Returns as soon as the provider accepts the request -- the VM may still
        be PENDING; poll `get_vm` (or wait for the worker's ready callback)."""
        raise NotImplementedError()

    async def get_vm(self, provider_id: str) -> VM | None:
        """Current provider-side state, or None if it no longer exists."""
        raise NotImplementedError()

    async def delete_vm(self, provider_id: str) -> None:
        """Destroy the VM. Must be idempotent: deleting a missing VM succeeds."""
        raise NotImplementedError()

    async def list_vms(self, tags: dict[str, str] | None = None) -> list[VM]:
        """VMs carrying all of `tags` -- used to find orphans."""
        raise NotImplementedError()

    async def run_cmd(self, vm_hostname: str, *cmd: str, cwd: str = ".") -> None:
        """Run a command on the VM over SSH. Only needed by the SSH bootstrap
        strategy and for debugging."""
        raise NotImplementedError()
