from typing import Protocol


class DNSManager(Protocol):
    """Keeps a stable name pointed at the current worker VM, so agents (which
    dial a hostname, not an IP) find a freshly booted one. Provider-agnostic:
    a second DNS host is one more adapter."""

    async def upsert_a(self, name: str, ip: str, ttl: int = 60) -> None:
        """Create or repoint the A record `name` -> `ip`. Idempotent."""
        ...

    async def delete_a(self, name: str, ip: str | None = None) -> None:
        """Remove the A record. With `ip`, only if it still points there, so a
        slow teardown can't delete the record a newer worker now owns."""
        ...
