"""A ConnWorkerClient look-alike for on-demand workers.

Services written against the static client (ConnectionService, StreamService)
keep their code: each call resolves the currently ready worker and delegates.
When there is none, calls raise WorkerUnavailableError, and `available()` lets
the sync loops skip a tick instead -- an empty listing would otherwise read as
"every connection is gone".
"""

from cloud_api.lib.conn_worker_client.conn_worker_client import ConnWorkerClient
from cloud_api.models.api import (
    CloseStreamResponse,
    Connection,
    Stream,
    TerminateConnResponse,
)
from cloud_api.models.api.worker import WorkerType
from cloud_api.service.worker_service import WorkerService, WorkerUnavailableError


class DynamicConnWorkerClient:
    def __init__(
        self,
        worker_service: WorkerService,
        worker_type: WorkerType = WorkerType.CONNECTION_WORKER,
    ):
        self._workers = worker_service
        self._type = worker_type

    async def _client(self) -> ConnWorkerClient:
        worker = await self._workers.get_ready_worker(self._type)
        if worker is None:
            raise WorkerUnavailableError("no conn_worker is running")
        return await self._workers.get_client(worker)

    async def available(self) -> bool:
        return await self._workers.get_ready_worker(self._type) is not None

    async def new_connection(
        self, node_id: str, name: str, id: str | None = None
    ) -> Connection:
        return await (await self._client()).new_connection(node_id, name, id=id)

    async def get_connection(self, connection_id: str) -> Connection:
        return await (await self._client()).get_connection(connection_id)

    async def terminate_connection(self, connection_id: str) -> TerminateConnResponse:
        return await (await self._client()).terminate_connection(connection_id)

    async def list_connections(self) -> list[Connection]:
        return await (await self._client()).list_connections()

    async def new_stream(
        self, connection_id: str, type: str, endpoint: str, port: int
    ) -> Stream:
        return await (await self._client()).new_stream(
            connection_id, type, endpoint, port
        )

    async def close_stream(self, stream_id: str) -> CloseStreamResponse:
        return await (await self._client()).close_stream(stream_id)

    async def list_streams(self) -> list[Stream]:
        return await (await self._client()).list_streams()

    async def get_stream(self, stream_id: str) -> Stream:
        return await (await self._client()).get_stream(stream_id)

    async def close(self) -> None:
        await self._workers.close()
