import asyncio

import pytest

from cloud_api.lib.events import Event, EventBus

pytestmark = pytest.mark.asyncio


async def test_subscription_registered_before_publish_receives_event():
    bus = EventBus()
    with bus.subscription("t") as sub:
        await bus.publish(Event("t", {"n": 1}))
        event = await sub.get(timeout=1)
    assert event is not None and event.payload == {"n": 1}


async def test_get_times_out_with_none():
    bus = EventBus()
    with bus.subscription("t") as sub:
        assert await sub.get(timeout=0.01) is None


async def test_topics_are_isolated_and_fan_out():
    bus = EventBus()
    with (
        bus.subscription("a") as a1,
        bus.subscription("a") as a2,
        bus.subscription("b") as b,
    ):
        await bus.publish(Event("a"))
        assert await a1.get(timeout=1) is not None
        assert await a2.get(timeout=1) is not None
        assert await b.get(timeout=0.01) is None


async def test_close_unregisters_so_nothing_leaks():
    bus = EventBus()
    with bus.subscription("t"):
        assert bus._subscriptions["t"]
    assert "t" not in bus._subscriptions


async def test_slow_subscriber_drops_oldest_instead_of_growing():
    bus = EventBus(maxsize=2)
    with bus.subscription("t") as sub:
        for n in range(5):
            await bus.publish(Event("t", {"n": n}))
        got = [(await sub.get(timeout=1)).payload["n"] for _ in range(2)]
        assert await sub.get(timeout=0.01) is None
    assert got == [3, 4]


async def test_subscribe_generator_cleans_up_on_close():
    bus = EventBus()
    gen = bus.subscribe("t")
    task = asyncio.create_task(gen.__anext__())
    await asyncio.sleep(0)  # let it register
    await bus.publish(Event("t", {"x": 1}))
    assert (await task).payload == {"x": 1}
    await gen.aclose()
    assert "t" not in bus._subscriptions
