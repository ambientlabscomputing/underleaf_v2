class FakeDNSManager:
    """In-memory records for tests and cloud-free local runs."""

    def __init__(self) -> None:
        self.records: dict[str, str] = {}
        self.fail_next_upsert: Exception | None = None

    async def upsert_a(self, name: str, ip: str, ttl: int = 60) -> None:
        if self.fail_next_upsert is not None:
            err, self.fail_next_upsert = self.fail_next_upsert, None
            raise err
        self.records[name] = ip

    async def delete_a(self, name: str, ip: str | None = None) -> None:
        if ip is None or self.records.get(name) == ip:
            self.records.pop(name, None)
