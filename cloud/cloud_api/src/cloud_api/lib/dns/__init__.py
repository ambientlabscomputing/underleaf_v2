from cloud_api.lib.dns.cloudflare import CloudflareDNSManager
from cloud_api.lib.dns.fake import FakeDNSManager
from cloud_api.lib.dns.protocol import DNSManager

__all__ = [
    "CloudflareDNSManager",
    "DNSManager",
    "FakeDNSManager",
    "dns_manager_factory",
]


def dns_manager_factory(worker_defaults) -> DNSManager | None:
    """None when DNS isn't configured (no names to keep, or no credentials);
    the worker is then reachable only by IP. `worker_defaults` is
    config.WorkerDefaults (untyped to keep lib/ free of a config import)."""
    cf = worker_defaults.cloudflare
    if not (cf.api_token and cf.zone_id):
        return None
    return CloudflareDNSManager(cf.api_token, cf.zone_id)
