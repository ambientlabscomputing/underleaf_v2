# RFD 4: Pricing, Allowances & Overage Policy

This RFD formalizes Track 5 of [ROADMAP.md](../ROADMAP.md). It records the product decisions (tiers, overage behaviour) and the engineering decisions that make them hold up under concurrency, retries, and partial failure. The numbers live in one place — [`cloud/cloud_api/src/cloud_api/lib/catalog.py`](../cloud/cloud_api/src/cloud_api/lib/catalog.py) — which seeds the `plans` / `plan_prices` tables and is linked to Stripe by `make stripe-sync`.

## What is sold

Underleaf prices by **data relayed through the Cloud Gateway** (bytes in + out, summed, counted at the gateway bridge). Connection slots are not a priced dimension.

| Tier | Price / month | Included data |
| ---- | ------------- | ------------- |
| Free | $0 | 5 GB |
| Maker | $9 | 100 GB |
| Builder | $24 | 500 GB |
| Studio | $59 | 2 TB |
| Fleet | $119 | 5 TB |

Sizes are decimal (1 GB = 10⁹ bytes). A prepaid **top-up block** is 100 GB for $5 on every paid tier.

## Overage policy

| Scenario | Behaviour |
| -------- | --------- |
| Free user reaches allowance | Hard stop on gateway-relayed traffic |
| Paid user reaches 80% | Usage notification |
| Paid user reaches 95% | Warning, and an offer to enable auto top-up |
| Paid user reaches 100% | A small, temporary grace allowance is applied |
| Auto top-up enabled | Charge for another prepaid data block |
| Auto top-up disabled | Stop relayed traffic when grace is exhausted |
| Monthly spending cap reached | Stop automatic purchases; same limited grace policy |

Auto top-up is **opt-in** (off by default) and offered prominently in the Usage page. "Hard stop" means every active stream for the account is closed at conn_worker and new streams are refused (HTTP 402) until a renewal or top-up replenishes the allowance. Manual top-ups ignore the spending cap — the cap limits *automatic* purchases only.

## Assumptions (not specified by the pricing sheet — change in `catalog.py`)

- **Free allowance is monthly** (calendar month, UTC), not lifetime.
- **Grace = 2% of the plan's included data, floor 1 GB** (Maker 2 GB … Fleet 100 GB). Granted once per exhaustion event.
- **Connection slots:** Free 1; every paid plan 25 (non-binding guard against runaway creation).
- **Default auto top-up cap: $50 per billing period**, customer-editable.
- Top-up price/size is identical on every paid tier.

## Mechanics

**Allowance & draw.** Each billing account has one `entitlements_bucket`: `network_traffic_allowance` (this period's total, incl. credited top-ups — the denominator for "% used") and `network_traffic_balance` (remaining). Traffic is charged with a single conditional `UPDATE` (balance first, then grace), so concurrent charges can't lose updates or go negative.

**Metering.** conn_worker counts bytes at the gateway bridge. A background loop in cloud_api (`billing.usage_sync_interval_seconds`, default 30s) turns the cumulative counters into `usage_events` and charges the bucket. Charging is **at-most-once**: the usage event (unique on `idempotency_key`) is written first and the charge only happens for the call that created it — a crash can under-charge one delta, never double-charge. Enforcement therefore lags by up to one interval; the grace allowance is sized to absorb that.

**Grace.** Granted by an atomic conditional update (`balance = 0 AND grace_granted_at IS NULL`), so it fires exactly once per exhaustion. A tick that carries the account *past* zero charges the overshoot to grace. A credited top-up or a renewal re-arms it.

**Refill.**
- *Paid:* only a **paid** renewal invoice (`invoice.paid`, `billing_reason = subscription_cycle`) refills the allowance — never the calendar, so a failed payment doesn't grant a new month. Replays are no-ops (the period must actually have advanced).
- *Free:* no Stripe subscription announces a renewal, so the bucket refills lazily the first time it is touched after the month ends.
- *Mid-period plan change:* usage is preserved (`used = allowance − balance` carries over), so upgrading can't reset usage and downgrading can't strand the balance.
- *Cancellation:* returns to the Free plan (hard-stop policy, no top-ups).

**Top-ups.** A dedicated one-off Stripe invoice (never a pending invoice item that could ride along on the next renewal). Credit is applied **only when Stripe reports it paid**, via an atomic `pending → paid` transition, so a replayed webhook can't credit twice. At most one top-up may be pending per account. Only subscriptions in good standing (`active`/`trialing`) can buy.

**Alerts.** 80/95/100% crossings and top-up cap/failure events are recorded once per period (unique constraint). They are surfaced through `GET /billing-accounts/{id}/usage` and the Usage page.

**Webhooks.** Verified by Stripe signature, deduplicated by Stripe event id — but a *failed* attempt is retried rather than swallowed as a duplicate.

## Security properties

Customers cannot write their own entitlements, usage events, or the plan catalog (no such routes; the catalog is read-only and managed by migration / `stripe-sync`). The customer-facing billing-account PATCH cannot set `stripe_customer_id` (which would let someone point top-ups at another customer's Stripe customer). Money-moving endpoints (`checkout-session`, `top-ups`, `usage`, `invoices`, `PATCH`) verify the caller owns the billing account and answer 404 for anyone else's. The connection and stream endpoints likewise 404 on resources owned by another tenant, and creating a connection/stream against another tenant's node/connection is refused, so one tenant can no longer consume another's allowance.

## Known limitations

- **No notification delivery.** Alerts are shown in the UI and logged; there is no email/Slack sender in the codebase yet.
- **Off-session top-ups depend on a default payment method** being present on the Stripe customer. Verified against stubbed Stripe calls only — exercise it once against Stripe test mode before launch.
- **Metering granularity** is the sync interval (see above); sub-interval overshoot beyond grace is not charged.
- **Hard stop closes streams** rather than throttling; the customer re-creates them after replenishing.
