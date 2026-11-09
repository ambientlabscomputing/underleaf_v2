"""
DeviceKeyService -- `ufctl` device keys (RFD 5, "ufctl connect").

A device key is an opaque bearer secret bound to one account:

    ufdk_<43 url-safe base64 chars>        256 bits from `secrets`

* Shown once, when `ufctl login` collects it. Only its SHA-256 is stored. A
  fast hash is right here because the secret has 256 bits of entropy; there is
  nothing to brute force (unlike a password).
* Recognisable on purpose: the `ufdk_` prefix is what tells the remote-auth
  endpoint "this is a device key and nothing else" (an OAuth JWT starts with
  `eyJ`, and a token minted from X-Subject-Id is a JWT), and what lets secret
  scanners find a leaked one.
* Revocable by its owner at any time; a revoked key never authenticates again.
"""

import hashlib
import hmac
import re
import secrets
from datetime import UTC, datetime, timedelta

from cloud_api import logger
from cloud_api.models.api.ufctl import DeviceKey
from cloud_api.models.base import IDPrefix, generate_id
from cloud_api.repository.device_key_repository import DeviceKeyRepository

DEVICE_KEY_PREFIX = "ufdk_"
_SECRET_BYTES = 32
# ufdk_ + 43 base64url chars. Anything else is rejected before the database.
_FORMAT = re.compile(r"^ufdk_[A-Za-z0-9_-]{43}$")
# last_used_at is a "last seen around" hint, not an audit log: write it at
# most this often per key so the hot path is not a write per request.
LAST_USED_THROTTLE = timedelta(minutes=5)
MAX_NAME_LENGTH = 64


def hash_device_key(secret: str) -> str:
    return hashlib.sha256(secret.encode()).hexdigest()


def looks_like_device_key(credential: str) -> bool:
    """True for anything that claims to be a device key. Such a credential is
    judged as a device key only, never as a JWT."""
    return credential.startswith(DEVICE_KEY_PREFIX)


def clean_device_name(raw: str) -> str:
    """The name is shown in the account UI: strip control characters and
    bound the length."""
    name = "".join(ch for ch in raw if ch.isprintable()).strip()
    return name[:MAX_NAME_LENGTH] or "unknown device"


def _to_api(row) -> DeviceKey:
    return DeviceKey.model_validate(row)


class DeviceKeyService:
    def __init__(self, device_key_repo: DeviceKeyRepository) -> None:
        self.device_key_repo = device_key_repo

    async def create_for_account(
        self, principal_account_id: str, name: str
    ) -> tuple[DeviceKey, str]:
        """Mint a key. Returns the stored record and the secret, which exists
        nowhere else and must be handed to the device immediately."""
        secret = DEVICE_KEY_PREFIX + secrets.token_urlsafe(_SECRET_BYTES)
        row = await self.device_key_repo.create(
            id=generate_id(IDPrefix.DEVICE_KEY),
            principal_account_id=principal_account_id,
            name=clean_device_name(name),
            key_hash=hash_device_key(secret),
        )
        logger.bind(device_key_id=row.id, account_id=principal_account_id).info(
            "Device key created"
        )
        return _to_api(row), secret

    async def authenticate(
        self, credential: str, *, now: datetime | None = None
    ) -> DeviceKey | None:
        """The key for `credential`, or None when it is malformed, unknown or
        revoked. Never raises for a bad credential and never logs it."""
        if not _FORMAT.match(credential):
            return None
        digest = hash_device_key(credential)
        row = await self.device_key_repo.get_by_hash(digest)
        if row is None:
            return None
        # The lookup already matched on the hash; compare again in constant
        # time so the decision does not depend on how the database compared.
        if not hmac.compare_digest(row.key_hash.encode(), digest.encode()):
            return None
        if row.revoked_at is not None:
            return None
        now = now or datetime.now(UTC)
        await self._touch(row, now)
        return _to_api(row)

    async def _touch(self, row, now: datetime) -> None:
        last = row.last_used_at
        if last is not None:
            if last.tzinfo is None:
                last = last.replace(tzinfo=UTC)
            if now - last < LAST_USED_THROTTLE:
                return
        try:
            await self.device_key_repo.touch_last_used(
                row.id, now, now - LAST_USED_THROTTLE
            )
        except Exception:  # noqa: BLE001  bookkeeping must never fail an authentication
            logger.bind(device_key_id=row.id).warning(
                "Could not update device key last_used_at"
            )

    async def list_for_account(self, principal_account_id: str) -> list[DeviceKey]:
        rows = await self.device_key_repo.list_active_for_account(principal_account_id)
        return [_to_api(r) for r in rows]

    async def revoke(self, principal_account_id: str, key_id: str) -> bool:
        """False when the account has no such key (foreign ids look the same
        as missing ones)."""
        ok = await self.device_key_repo.revoke(key_id, principal_account_id)
        if ok:
            logger.bind(device_key_id=key_id, account_id=principal_account_id).info(
                "Device key revoked"
            )
        return ok
