"""
DeviceLoginService -- the RFC 8628 device authorization grant behind
`ufctl login` (RFD 5, "ufctl connect"). Shaped like RegistrationService:

  POST /ufctl/device           ufctl starts a login          (no user auth)
  POST /ufctl/token            ufctl polls for the result    (no user auth)
  GET  /ufctl/device/request   account UI reads the request  (user auth)
  POST /ufctl/device/approve   owner approves                (user auth)
  POST /ufctl/device/deny      owner denies                  (user auth)

Properties:
* The device_code is 256 random bits and only its SHA-256 is stored.
* The user_code is shown to the owner, so it is short (8 characters from an
  alphabet without look-alikes). Guessing is bounded by a short life (10
  minutes) and by a per-account limit on failed look-ups.
* The request is single use: it is consumed with one conditional UPDATE, so of
  any number of concurrent polls exactly one receives the device key.
* Polling faster than `interval` answers slow_down and raises the interval.
* Every error that would tell an attacker whether a device_code ever existed
  (unknown, consumed, expired) is the same expired_token.
"""

import hashlib
import secrets
import time
from collections import defaultdict, deque
from datetime import UTC, datetime, timedelta

from cloud_api import app_config, logger
from cloud_api.models.api.ufctl import (
    DeviceLoginDecisionResponse,
    DeviceLoginRequestInfo,
    DeviceLoginStatus,
    UfctlDeviceRequest,
    UfctlDeviceResponse,
    UfctlTokenResponse,
)
from cloud_api.models.base import IDPrefix, generate_id
from cloud_api.repository.device_login_repository import DeviceLoginRepository
from cloud_api.service.device_key_service import DeviceKeyService, clean_device_name

DEVICE_CODE_TTL_SECONDS = 600
POLL_INTERVAL_SECONDS = 5
MAX_POLL_INTERVAL_SECONDS = 60
# A client polling exactly on its interval can arrive a hair early.
_POLL_SLACK = timedelta(milliseconds=500)
_DEVICE_CODE_BYTES = 32
# No 0/O, 1/I/L: the owner copies this by eye.
_USER_CODE_ALPHABET = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
_USER_CODE_PART = 4

# Unauthenticated device creation is capped across the process.
_MAX_STARTS_PER_MINUTE = 120
# Failed user_code look-ups an account may make per window.
_MAX_FAILED_LOOKUPS = 10
_FAILED_LOOKUP_WINDOW_SECONDS = 600


class DeviceLoginError(Exception):
    """RFC 8628 token endpoint error; `code` is the `error` value."""

    def __init__(self, code: str, description: str = "") -> None:
        super().__init__(code)
        self.code = code
        self.description = description


class DeviceLoginNotFoundError(Exception):
    """No (live) login request for this user_code."""


class DeviceLoginExpiredError(Exception):
    """The request expired."""


class DeviceLoginNotPendingError(Exception):
    """The request was already approved, denied or used."""


class TooManyAttemptsError(Exception):
    """Rate limit hit (device starts, or failed user_code look-ups)."""


class _SlidingWindow:
    """Tiny in-process event counter. cloud_api is a single process (see the
    assertion key storage note); a replica set would need this in shared
    storage."""

    def __init__(self, limit: int, window_seconds: float, clock=time.monotonic):
        self.limit = limit
        self.window = window_seconds
        self.clock = clock
        self._events: dict[str, deque[float]] = defaultdict(deque)

    def _prune(self, key: str) -> deque[float]:
        q = self._events[key]
        cutoff = self.clock() - self.window
        while q and q[0] <= cutoff:
            q.popleft()
        return q

    def exhausted(self, key: str) -> bool:
        return len(self._prune(key)) >= self.limit

    def record(self, key: str) -> None:
        self._prune(key).append(self.clock())


def _sha256(value: str) -> str:
    return hashlib.sha256(value.encode()).hexdigest()


def generate_user_code() -> str:
    def part() -> str:
        return "".join(
            secrets.choice(_USER_CODE_ALPHABET) for _ in range(_USER_CODE_PART)
        )

    return f"{part()}-{part()}"


def normalize_user_code(raw: str) -> str:
    """Accept what a person types: any case, with or without the hyphen,
    surrounding spaces. Unusable input is returned unchanged (and so never
    matches)."""
    compact = "".join(ch for ch in raw.upper() if ch not in " -\t")
    if len(compact) != 2 * _USER_CODE_PART:
        return raw
    return f"{compact[:_USER_CODE_PART]}-{compact[_USER_CODE_PART:]}"


def _aware(dt: datetime) -> datetime:
    return dt if dt.tzinfo is not None else dt.replace(tzinfo=UTC)


class DeviceLoginService:
    def __init__(
        self,
        device_login_repo: DeviceLoginRepository,
        device_key_service: DeviceKeyService,
    ) -> None:
        self.repo = device_login_repo
        self.device_key_service = device_key_service
        self._starts = _SlidingWindow(_MAX_STARTS_PER_MINUTE, 60)
        self._failed_lookups = _SlidingWindow(
            _MAX_FAILED_LOOKUPS, _FAILED_LOOKUP_WINDOW_SECONDS
        )

    def _verification_uri(self) -> str:
        if not app_config:
            raise RuntimeError("App config not loaded")
        return f"{app_config.account_ui_base_url.rstrip('/')}/ufctl/activate"

    # ----------------------------------------------------------- ufctl side

    async def start(self, req: UfctlDeviceRequest) -> UfctlDeviceResponse:
        if self._starts.exhausted("global"):
            raise TooManyAttemptsError("Too many login requests")
        self._starts.record("global")

        device_code = secrets.token_hex(_DEVICE_CODE_BYTES)
        user_code = generate_user_code()
        now = datetime.now(UTC)
        row = await self.repo.create(
            id=generate_id(IDPrefix.DEVICE_LOGIN),
            user_code=user_code,
            device_code_hash=_sha256(device_code),
            device_name=clean_device_name(req.device_name),
            expires_at=now + timedelta(seconds=DEVICE_CODE_TTL_SECONDS),
            poll_interval=POLL_INTERVAL_SECONDS,
        )
        # The device_code is a secret: only the user_code and ids are logged.
        logger.bind(device_login_id=row.id, user_code=user_code).info(
            "ufctl device login started"
        )
        uri = self._verification_uri()
        return UfctlDeviceResponse(
            device_code=device_code,
            user_code=user_code,
            verification_uri=uri,
            verification_uri_complete=f"{uri}?user_code={user_code}",
            expires_in=DEVICE_CODE_TTL_SECONDS,
            interval=POLL_INTERVAL_SECONDS,
        )

    async def poll(
        self, device_code: str, *, now: datetime | None = None
    ) -> UfctlTokenResponse:
        """Exchange an approved device_code for a device key, once.

        Raises DeviceLoginError with one of authorization_pending, slow_down,
        expired_token, access_denied."""
        now = now or datetime.now(UTC)
        row = await self.repo.get_by_device_code_hash(_sha256(device_code))
        if row is None or row.status == DeviceLoginStatus.CONSUMED:
            raise DeviceLoginError("expired_token")
        if now >= _aware(row.expires_at):
            raise DeviceLoginError("expired_token")

        # Rate limit before anything is revealed or consumed. A too-fast poll
        # does not move last_polled_at (it must wait out the full interval)
        # but does raise the interval, as RFC 8628 section 3.5 asks.
        interval = timedelta(seconds=row.poll_interval)
        if (
            row.last_polled_at is not None
            and now - _aware(row.last_polled_at) < interval - _POLL_SLACK
        ):
            await self.repo.raise_poll_interval(
                row.id,
                min(row.poll_interval + 5, MAX_POLL_INTERVAL_SECONDS),
                now,
            )
            raise DeviceLoginError("slow_down")
        await self.repo.record_poll(row.id, now, row.poll_interval)

        if row.status == DeviceLoginStatus.PENDING:
            raise DeviceLoginError("authorization_pending")
        if row.status == DeviceLoginStatus.DENIED:
            raise DeviceLoginError("access_denied")
        if row.status != DeviceLoginStatus.APPROVED or not row.principal_account_id:
            raise DeviceLoginError("expired_token")

        # approved: the first poll to flip it to consumed mints the key.
        if not await self.repo.consume(row.id, now):
            raise DeviceLoginError("expired_token")
        key, secret = await self.device_key_service.create_for_account(
            row.principal_account_id, row.device_name
        )
        logger.bind(device_login_id=row.id, device_key_id=key.id).info(
            "ufctl device login completed"
        )
        return UfctlTokenResponse(device_key_id=key.id, device_key=secret)

    # ------------------------------------------------------- account UI side

    async def get_request(
        self, user_code: str, account_id: str, *, now: datetime | None = None
    ) -> DeviceLoginRequestInfo:
        row = await self._lookup(user_code, account_id, now)
        return DeviceLoginRequestInfo(
            user_code=row.user_code,
            device_name=row.device_name,
            status=DeviceLoginStatus(row.status),
            created_at=row.created_at,
            expires_at=row.expires_at,
        )

    async def approve(
        self, user_code: str, account_id: str, *, now: datetime | None = None
    ) -> DeviceLoginDecisionResponse:
        return await self._decide(
            user_code, account_id, DeviceLoginStatus.APPROVED, now
        )

    async def deny(
        self, user_code: str, account_id: str, *, now: datetime | None = None
    ) -> DeviceLoginDecisionResponse:
        return await self._decide(user_code, account_id, DeviceLoginStatus.DENIED, now)

    async def _lookup(self, user_code: str, account_id: str, now: datetime | None):
        """The live request for a user_code. Failed look-ups are counted per
        account so a signed-in user cannot walk the code space."""
        if self._failed_lookups.exhausted(account_id):
            raise TooManyAttemptsError("Too many failed attempts")
        row = await self.repo.get_by_user_code(normalize_user_code(user_code))
        if row is None:
            self._failed_lookups.record(account_id)
            raise DeviceLoginNotFoundError("No such login request")
        if (now or datetime.now(UTC)) >= _aware(row.expires_at):
            raise DeviceLoginExpiredError("Login request expired")
        return row

    async def _decide(
        self,
        user_code: str,
        account_id: str,
        decision: DeviceLoginStatus,
        now: datetime | None,
    ) -> DeviceLoginDecisionResponse:
        now = now or datetime.now(UTC)
        row = await self._lookup(user_code, account_id, now)
        if row.status != DeviceLoginStatus.PENDING:
            raise DeviceLoginNotPendingError("Login request is not pending")
        if not await self.repo.decide(row.id, decision.value, account_id, now):
            raise DeviceLoginNotPendingError("Login request is not pending")
        logger.bind(
            device_login_id=row.id, account_id=account_id, decision=decision.value
        ).info("ufctl device login decided")
        return DeviceLoginDecisionResponse(status=decision, device_name=row.device_name)
