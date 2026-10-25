import asyncio

from cloud_api import logger
from cloud_api.service_manager import get_connection_service, get_stream_service


async def run_usage_sync_loop(interval_seconds: int) -> None:
    """Meter conn_worker traffic and enforce allowances on a fixed cadence.

    Metering used to piggyback on API reads, so with nobody polling, traffic
    went unmetered and an exhausted account was never stopped. Connections
    sync first so streams have their parent rows.
    """
    while True:
        try:
            await get_connection_service().wait_for_sync()
            await get_stream_service().wait_for_sync()
        except asyncio.CancelledError:
            raise
        except Exception as exc:  # noqa: BLE001 -- keep the loop alive
            logger.error(f"usage sync tick failed: {exc}")
        await asyncio.sleep(interval_seconds)
