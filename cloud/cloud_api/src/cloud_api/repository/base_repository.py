from cloud_api.repository import session
from contextlib import asynccontextmanager


class BaseRepository:
    def __init__(self):
        pass

    @asynccontextmanager
    async def get_session(self):
        async with session.get_session() as db_session:
            yield db_session
