from cloud_api.lib.vm_manager.digital_ocean import DOVMManager
from cloud_api.lib.vm_manager.fake import FakeVMManager
from cloud_api.lib.vm_manager.models import VM, VMProvider, VMSpec, VMStatus
from cloud_api.lib.vm_manager.protocol import VMManager

__all__ = [
    "VM",
    "DOVMManager",
    "FakeVMManager",
    "VMManager",
    "VMProvider",
    "VMSpec",
    "VMStatus",
    "vm_manager_factory",
]


def vm_manager_factory(provider: VMProvider, worker_defaults=None) -> VMManager:
    """`worker_defaults` is config.WorkerDefaults (untyped here to keep lib/
    free of a config import cycle)."""
    match provider:
        case VMProvider.DIGITAL_OCEAN:
            do = worker_defaults.digital_ocean if worker_defaults else None
            if do is None:
                return DOVMManager(token="")
            return DOVMManager(
                token=do.token,
                region=do.region,
                image=do.image,
                ssh_key_ids=do.ssh_key_ids,
            )
        case VMProvider.FAKE:
            return FakeVMManager()
        case _:
            raise RuntimeError(f"Unknown VM provider: {provider}")
