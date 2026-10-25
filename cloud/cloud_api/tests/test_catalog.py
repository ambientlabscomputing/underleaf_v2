from datetime import UTC, datetime

from cloud_api.lib import catalog

GB, TB = 10**9, 10**12


def test_tiers_match_the_published_pricing():
    assert [
        (p.name, p.price_cents, p.included_bytes) for p in catalog.PLANS if p.paid
    ] == [
        ("maker", 900, 100 * GB),
        ("builder", 2400, 500 * GB),
        ("studio", 5900, 2 * TB),
        ("fleet", 11900, 5 * TB),
    ]


def test_free_tier_hard_stops_and_cannot_top_up():
    assert catalog.FREE.paid is False
    assert catalog.FREE.grace_bytes == 0
    assert catalog.FREE.top_up_block_bytes == 0 and catalog.FREE.top_up_price_cents == 0


def test_paid_tiers_get_a_small_grace_and_the_5_dollar_100gb_top_up():
    for plan in (catalog.MAKER, catalog.BUILDER, catalog.STUDIO, catalog.FLEET):
        assert plan.top_up_block_bytes == 100 * GB
        assert plan.top_up_price_cents == 500
        # "small": a couple of percent of the plan, never more than 5%.
        assert GB <= plan.grace_bytes <= plan.included_bytes * 0.05


def test_grace_scales_with_plan_but_has_a_floor():
    assert catalog.MAKER.grace_bytes == 2 * GB
    assert catalog.BUILDER.grace_bytes == 10 * GB
    tiny = catalog.PlanSpec("t", 100, 10 * GB, 1, paid=True)
    assert tiny.grace_bytes == catalog.GRACE_FLOOR_BYTES


def test_month_bounds_handles_december_rollover():
    start, end = catalog.month_bounds(datetime(2026, 12, 15, 9, 30, tzinfo=UTC))
    assert start == datetime(2026, 12, 1, tzinfo=UTC)
    assert end == datetime(2027, 1, 1, tzinfo=UTC)


def test_month_bounds_midyear():
    start, end = catalog.month_bounds(datetime(2026, 3, 31, 23, 59, tzinfo=UTC))
    assert (start.month, end.month) == (3, 4)
