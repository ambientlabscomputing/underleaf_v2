from cloud_api import logger
from cloud_api.service.entitlements_bucket_service import EntitlementsBucketService
from cloud_api.service.top_up_service import TopUpService


class UsageEnforcementService:
    """The overage policy (RFDs/RFD-4.md) applied to a metered traffic charge.

    Thresholds, the one-time grace grant, and hard-stop detection live in
    EntitlementsBucketService (they must be atomic with the draw); this layer
    reacts to them -- notably by buying a top-up when the allowance runs out
    and the customer opted in to auto top-up.
    """

    def __init__(
        self,
        entitlements_bucket_service: EntitlementsBucketService,
        top_up_service: TopUpService,
    ) -> None:
        self.bucket_service = entitlements_bucket_service
        self.top_up_service = top_up_service

    async def record_traffic(self, billing_account_id: str, amount: int) -> bool:
        """Charge `amount` bytes. Returns True when gateway-relayed traffic
        for this account must be stopped."""
        outcome = await self.bucket_service.charge_network_traffic(
            billing_account_id, amount
        )
        if outcome is None:
            return False

        for kind in outcome.new_alerts:
            logger.bind(billing_account_id=billing_account_id, alert=kind.value).info(
                "Usage alert raised"
            )

        if outcome.grace_granted:
            # 100% reached on a paid plan: the grace allowance bridges the gap
            # while an opted-in auto top-up is paid for. Exactly once per
            # exhaustion event -- grant_grace is atomic.
            await self.top_up_service.try_auto_top_up(billing_account_id)

        return outcome.hard_stop

    async def on_auto_top_up_enabled(self, billing_account_id: str) -> None:
        """If the customer turns auto top-up on while already out of data, the
        grace grant that normally triggers a purchase has come and gone --
        buy now instead of waiting for the next exhaustion."""
        bucket = await self.bucket_service.get_entitlements_bucket_by_billing_account(
            billing_account_id
        )
        if bucket is not None and bucket.network_traffic_balance == 0:
            await self.top_up_service.try_auto_top_up(billing_account_id)
