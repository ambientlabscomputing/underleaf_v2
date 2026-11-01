"""
AsyncIO queue based, no-poll, in-process event bus.

Events are wake-up hints, not a source of truth: they are lost on restart and
are never delivered across processes, so anything that must not be missed has
to be re-checked against the database too.
"""

import asyncio
from collections.abc import AsyncGenerator, AsyncIterator
from dataclasses import dataclass, field
from typing import Self


@dataclass
class Event:
    topic: str
    payload: dict = field(default_factory=dict)


class Subscription:
    """A registered listener on one topic.

    Registration happens on construction, so use it as a context manager
    *before* the work that will trigger the event -- otherwise an event
    published between "start waiting" and the first `get()` is missed.
    """

    def __init__(self, bus: "EventBus", topic: str, maxsize: int):
        self._bus = bus
        self._topic = topic
        self._queue: asyncio.Queue[Event] = asyncio.Queue(maxsize=maxsize)
        bus._register(topic, self)

    def _deliver(self, event: Event) -> None:
        # Bounded and lossy by design: a slow subscriber drops its oldest
        # event rather than blocking publishers or growing without limit.
        while True:
            try:
                self._queue.put_nowait(event)
                return
            except asyncio.QueueFull:
                self._queue.get_nowait()

    async def get(self, timeout: float | None = None) -> Event | None:
        """Next event, or None if `timeout` seconds pass first."""
        try:
            return await asyncio.wait_for(self._queue.get(), timeout)
        except TimeoutError:
            return None

    def close(self) -> None:
        self._bus._unregister(self._topic, self)

    def __enter__(self) -> Self:
        return self

    def __exit__(self, *exc) -> None:
        self.close()

    def __aiter__(self) -> AsyncIterator[Event]:
        return self._iter()

    async def _iter(self) -> AsyncGenerator[Event]:
        try:
            while True:
                yield await self._queue.get()
        finally:
            self.close()


class EventBus:
    def __init__(self, maxsize: int = 100):
        self._maxsize = maxsize
        self._subscriptions: dict[str, set[Subscription]] = {}

    def _register(self, topic: str, sub: Subscription) -> None:
        self._subscriptions.setdefault(topic, set()).add(sub)

    def _unregister(self, topic: str, sub: Subscription) -> None:
        subs = self._subscriptions.get(topic)
        if subs is None:
            return
        subs.discard(sub)
        if not subs:
            del self._subscriptions[topic]

    def subscription(self, topic: str) -> Subscription:
        """Register now; read with `get(timeout)` or `async for`. Close it
        (or use `with`) when done."""
        return Subscription(self, topic, self._maxsize)

    async def publish(self, event: Event) -> None:
        for sub in list(self._subscriptions.get(event.topic, ())):
            sub._deliver(event)

    async def subscribe(self, topic: str) -> AsyncGenerator[Event]:
        """Yield events as they arrive. The listener is removed when the
        generator is closed. Registration happens on the first `__anext__`,
        so prefer `subscription()` when an event could fire before then."""
        with self.subscription(topic) as sub:
            async for event in sub:
                yield event
