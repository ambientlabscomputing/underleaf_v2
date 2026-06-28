from aiohttp import ClientSession

from cloud_api.models.api import (
    Connection,
    CreateConnectionRequest,
    Stream,
    CreateConnectionRequest,
    NewStreamRequest,
    TerminateConnResponse,
    CloseStreamResponse,
    TerminateConnRequest,
    CloseStreamRequest,
)


class ConnWorkerClient:
    def __init__(self, base_url: str):
        self.base_url = base_url
        self.session = ClientSession()

    async def _post(self, endpoint: str, json_data: dict) -> dict:
        url = f"{self.base_url}/{endpoint}"
        async with self.session.post(url, json=json_data) as response:
            response.raise_for_status()
            return await response.json()

    async def _get(self, endpoint: str) -> dict:
        url = f"{self.base_url}/{endpoint}"
        async with self.session.get(url) as response:
            response.raise_for_status()
            return await response.json()

    async def new_connection(self, req: CreateConnectionRequest) -> Connection:
        data = await self._post("connections", req.model_dump(mode="json"))
        return Connection.model_validate(data)

    async def terminate_connection(
        self, req: TerminateConnRequest
    ) -> TerminateConnResponse:
        data = await self._post(
            f"connections/{req.connection_id}/terminate", req.model_dump(mode="json")
        )
        return TerminateConnResponse.model_validate(data)

    async def new_stream(self, req: NewStreamRequest) -> Stream:
        data = await self._post("streams", req.model_dump(mode="json"))
        return Stream.model_validate(data)

    async def close_stream(self, req: CloseStreamRequest) -> CloseStreamResponse:
        data = await self._post(
            f"streams/{req.stream_id}/close", req.model_dump(mode="json")
        )
        return CloseStreamResponse.model_validate(data)

    async def get_connection(self, connection_id: str) -> Connection:
        data = await self._get(f"connections/{connection_id}")
        return Connection.model_validate(data)

    async def list_connections(self) -> list[Connection]:
        data = await self._get("connections")
        return [Connection.model_validate(item) for item in data]

    async def list_streams(self) -> list[Stream]:
        data = await self._get("streams")
        return [Stream.model_validate(item) for item in data]

    async def get_stream(self, stream_id: str) -> Stream:
        data = await self._get(f"streams/{stream_id}")
        return Stream.model_validate(data)

    async def close(self):
        await self.session.close()
