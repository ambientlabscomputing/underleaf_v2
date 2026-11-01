"""Runtime overrides for a few worker settings.

The config file stays the durable default; an operator can override a handful
of knobs (above all the provisioning kill switch) without a redeploy. Overrides
live in the database so they survive restarts and reach every process that
polls them -- the API's loops and `workerctl` alike.
"""

import time

from cloud_api.config import WorkerDefaults
from cloud_api.repository.worker_settings_repository import WorkerSettingsRepository

# name -> (type, minimum for ints)
OVERRIDABLE: dict[str, tuple[type, int | None]] = {
    "provisioning_enabled": (bool, None),
    "idle_grace_seconds": (int, 0),
    "task_timeout_seconds": (int, 1),
    "max_vm_lifetime_hours": (int, 0),
}


class InvalidSettingError(ValueError):
    pass


class WorkerSettingsService:
    def __init__(
        self,
        repo: WorkerSettingsRepository,
        settings: WorkerDefaults,
        refresh_seconds: float = 5.0,
    ):
        self.repo = repo
        self.settings = settings  # the live object every service reads
        self._defaults = {k: getattr(settings, k) for k in OVERRIDABLE}
        self._refresh_seconds = refresh_seconds
        self._last_refresh = float("-inf")

    async def refresh(self, force: bool = False) -> None:
        """Apply stored overrides to the live settings (rate-limited; the loops
        call this every tick)."""
        now = time.monotonic()
        if not force and now - self._last_refresh < self._refresh_seconds:
            return
        self._last_refresh = now
        self._apply(await self.repo.get_all())

    def _apply(self, overrides: dict[str, object]) -> None:
        for key, default in self._defaults.items():
            setattr(self.settings, key, overrides.get(key, default))

    async def describe(self) -> dict:
        stored = await self.repo.get_all()
        self._apply(stored)  # report what is in force, not a stale snapshot
        overrides = {k: v for k, v in stored.items() if k in OVERRIDABLE}
        return {
            "effective": {k: getattr(self.settings, k) for k in OVERRIDABLE},
            "overrides": overrides,
            "defaults": dict(self._defaults),
        }

    async def update(self, changes: dict[str, object]) -> dict:
        """Set overrides; a value of None clears that override (back to the
        config default). All-or-nothing: nothing is written if any is invalid."""
        for key, value in changes.items():
            if key not in OVERRIDABLE:
                raise InvalidSettingError(f"{key!r} is not an overridable setting")
            if value is None:
                continue
            kind, minimum = OVERRIDABLE[key]
            if kind is bool:
                if not isinstance(value, bool):
                    raise InvalidSettingError(f"{key} must be true or false")
            else:
                if isinstance(value, bool) or not isinstance(value, int):
                    raise InvalidSettingError(f"{key} must be an integer")
                if minimum is not None and value < minimum:
                    raise InvalidSettingError(f"{key} must be >= {minimum}")
        for key, value in changes.items():
            if value is None:
                await self.repo.unset(key)
            else:
                await self.repo.set(key, value)
        await self.refresh(force=True)
        return await self.describe()
