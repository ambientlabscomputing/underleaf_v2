import asyncio

from cloud_api import logger
from cloud_api.service.worker_service import WAKE_TOPIC
from cloud_api.service_manager import (
    get_event_bus,
    get_worker_service,
    get_worker_settings_service,
)


async def run_worker_reconciler(interval_seconds: int) -> None:
    """Advance worker VMs on a fixed cadence, or sooner when something asks
    (a new request, a VM callback). Safe to restart at any time: all state is
    in the database, so a restart just resumes whatever was mid-flight."""
    service = get_worker_service()
    with get_event_bus().subscription(WAKE_TOPIC) as wake:
        while True:
            try:
                await get_worker_settings_service().refresh()
                await service.reconcile()
            except asyncio.CancelledError:
                raise
            except Exception as exc:  # noqa: BLE001 -- keep the loop alive
                logger.error(f"worker reconcile tick failed: {exc}")
            await wake.get(timeout=interval_seconds)
