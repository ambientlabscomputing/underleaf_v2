"""Link the plan catalog to Stripe: `python -m cloud_api.scripts.sync_stripe_catalog`

Needs UNDERLEAF_STRIPE_API_KEY (use a test-mode key for non-production) and a
migrated database. Safe to re-run.
"""

import asyncio

import cloud_api
from cloud_api import load_config
from cloud_api.repository import session as db_session
from cloud_api.repository.plan_price_repository import PlanPriceRepository
from cloud_api.repository.plan_repository import PlanRepository
from cloud_api.service.stripe_catalog_sync import StripeCatalogSync


async def _main() -> None:
    cfg = load_config()
    cloud_api.app_config = cfg
    db_session.init_session_maker(cfg)
    try:
        linked = await StripeCatalogSync(
            PlanRepository(), PlanPriceRepository(), cfg.stripe
        ).run()
    finally:
        await db_session.get_engine().dispose()
    for name, price_id in linked.items():
        print(f"{name:<8} {price_id}")


if __name__ == "__main__":
    asyncio.run(_main())
