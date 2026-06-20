from cloud_api.models.api import (
    User,
    CreateUserRequest,
    QueryUserRequest,
    PatchUserRequest,
)
from cloud_api.models.sql import SQLUser, SQLUserPassword
from cloud_api.repository.base_repository import BaseRepository
from sqlalchemy import select, update
from datetime import datetime, timezone


class UserRepository(BaseRepository):
    async def create_user(
        self, req: CreateUserRequest, principal_account_id: str
    ) -> User:
        async with self.get_session() as session:
            new_user = SQLUser(
                name=req.name,
                email=req.email,
                principal_account_id=principal_account_id,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
            )
            session.add(new_user)
            await session.commit()
            await session.refresh(new_user)
            return User.model_validate(new_user)

    async def create_user_password(self, user_id: str, password_hash: str) -> None:
        async with self.get_session() as session:
            user_password = SQLUserPassword(
                user_id=user_id,
                password_hash=password_hash,
                created_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc),
            )
            session.add(user_password)
            await session.commit()

    async def get_user(self, user_id: str) -> User | None:
        async with self.get_session() as session:
            user = await session.get(SQLUser, user_id)
            if user is None:
                return None
            return User.model_validate(user)

    async def get_user_by_email(self, email: str) -> User | None:
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLUser).where(SQLUser.email == email)
            )
            user = result.first()
            if user is None:
                return None
            return User.model_validate(user)

    async def get_user_password_hash(self, user_id: str) -> str | None:
        async with self.get_session() as session:
            result = await session.scalars(
                select(SQLUserPassword).where(SQLUserPassword.user_id == user_id)
            )
            user_password = result.first()
            if user_password is None:
                return None
            return user_password.password_hash

    async def query_users(self, req: QueryUserRequest) -> list[User]:
        async with self.get_session() as session:
            query = select(SQLUser)
            if req.name is not None:
                query = query.filter(SQLUser.name == req.name)
            if req.email is not None:
                query = query.filter(SQLUser.email == req.email)
            result = await session.scalars(query)
            users = result.all()
            return [User.model_validate(user) for user in users]

    async def patch_user(self, user_id: str, req: PatchUserRequest) -> User | None:
        async with self.get_session() as session:
            update_stmt = update(SQLUser).where(SQLUser.id == user_id).values(updated_at=datetime.now(timezone.utc))
            if req.name is not None:
                update_stmt = update_stmt.values(name=req.name)
            if req.email is not None:
                update_stmt = update_stmt.values(email=req.email)
            result = await session.execute(update_stmt)
            if result.rowcount == 0:
                return None
            await session.commit()
            updated_user = await session.get(SQLUser, user_id)
            return User.model_validate(updated_user)

    async def delete_user(self, user_id: str) -> bool:
        async with self.get_session() as session:
            user = await session.get(SQLUser, user_id)
            if user is None:
                return False
            await session.delete(user)
            await session.commit()
            return True
