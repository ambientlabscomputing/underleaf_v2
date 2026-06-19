from cloud_api.models.api import (
    Node,
    CreateNodeRequest,
    QueryNodeRequest,
    PatchNodeRequest,
)
from cloud_api.models.sql import SQLNode
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update
from datetime import datetime, timezone


class NodeRepository(BaseRepository):
    async def create_node(self, req: CreateNodeRequest) -> Node:
        async with self.get_session() as session:
            new_node = SQLNode(
                id=req.id,
                name=req.name,
                cluster_id=req.cluster_id,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
            )
            session.add(new_node)
            await session.commit()
            await session.refresh(new_node)
            return Node.model_validate(new_node)

    async def get_node(self, node_id: str) -> Node | None:
        async with self.get_session() as session:
            node = await session.get(SQLNode, node_id)
            if node is None:
                return None
            return Node.model_validate(node)

    async def query_nodes(self, req: QueryNodeRequest) -> list[Node]:
        async with self.get_session() as session:
            query = select(SQLNode)
            if req.name is not None:
                query = query.filter(SQLNode.name == req.name)
            if req.cluster_id is not None:
                query = query.filter(SQLNode.cluster_id == req.cluster_id)
            result = await session.scalars(query)
            nodes = result.all()
            return [Node.model_validate(node) for node in nodes]

    async def patch_node(self, node_id: str, req: PatchNodeRequest) -> Node | None:
        async with self.get_session() as session:
            update_stmt = update(SQLNode).where(SQLNode.id == node_id).values(updated_at=datetime.now(timezone.utc))
            if req.name is not None:
                update_stmt = update_stmt.values(name=req.name)
            result = await session.execute(update_stmt)
            if result.scalar() is None:
                return None
            await session.commit()
            updated_node = await session.get(SQLNode, node_id)
            return Node.model_validate(updated_node)

    async def delete_node(self, node_id: str) -> bool:
        async with self.get_session() as session:
            node = await session.get(SQLNode, node_id)
            if node is None:
                return False
            await session.delete(node)
            await session.commit()
            return True
