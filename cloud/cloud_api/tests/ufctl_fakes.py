"""In-memory stand-ins for the device login / device key repositories. They
implement the same conditional transitions as the SQL (single use, owner
scoped), so the services' logic is tested without Postgres."""

from datetime import UTC, datetime
from types import SimpleNamespace


class FakeDeviceKeyRepo:
    def __init__(self):
        self.rows: dict[str, SimpleNamespace] = {}
        self.touches = 0

    async def create(self, id, principal_account_id, name, key_hash):
        now = datetime.now(UTC)
        row = SimpleNamespace(
            id=id,
            principal_account_id=principal_account_id,
            name=name,
            key_hash=key_hash,
            last_used_at=None,
            revoked_at=None,
            created_at=now,
            updated_at=now,
        )
        self.rows[id] = row
        return row

    async def get_by_hash(self, key_hash):
        return next((r for r in self.rows.values() if r.key_hash == key_hash), None)

    async def list_active_for_account(self, principal_account_id):
        return [
            r
            for r in self.rows.values()
            if r.principal_account_id == principal_account_id and r.revoked_at is None
        ]

    async def revoke(self, key_id, principal_account_id):
        row = self.rows.get(key_id)
        if row is None or row.principal_account_id != principal_account_id:
            return False
        if row.revoked_at is None:
            row.revoked_at = datetime.now(UTC)
        return True

    async def touch_last_used(self, key_id, now, older_than):
        row = self.rows[key_id]
        if row.last_used_at is None or row.last_used_at < older_than:
            row.last_used_at = now
            self.touches += 1


class FakeDeviceLoginRepo:
    def __init__(self):
        self.rows: dict[str, SimpleNamespace] = {}

    async def create(
        self, id, user_code, device_code_hash, device_name, expires_at, poll_interval
    ):
        now = datetime.now(UTC)
        row = SimpleNamespace(
            id=id,
            user_code=user_code,
            device_code_hash=device_code_hash,
            status="pending",
            device_name=device_name,
            principal_account_id=None,
            expires_at=expires_at,
            poll_interval=poll_interval,
            last_polled_at=None,
            created_at=now,
            updated_at=now,
        )
        self.rows[id] = row
        return row

    async def get_by_user_code(self, user_code):
        return next((r for r in self.rows.values() if r.user_code == user_code), None)

    async def get_by_device_code_hash(self, device_code_hash):
        return next(
            (r for r in self.rows.values() if r.device_code_hash == device_code_hash),
            None,
        )

    async def decide(self, request_id, status, principal_account_id, now):
        row = self.rows[request_id]
        if row.status != "pending" or row.expires_at <= now:
            return False
        row.status = status
        row.principal_account_id = principal_account_id
        return True

    async def consume(self, request_id, now):
        row = self.rows[request_id]
        if row.status != "approved" or row.expires_at <= now:
            return False
        row.status = "consumed"
        return True

    async def record_poll(self, request_id, now, poll_interval):
        row = self.rows[request_id]
        row.last_polled_at = now
        row.poll_interval = poll_interval

    async def raise_poll_interval(self, request_id, poll_interval, now):
        self.rows[request_id].poll_interval = poll_interval
