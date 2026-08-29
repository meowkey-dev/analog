"""SSE fan-out.

In-process only: one server, one machine, SPEC §3 binds to loopback. A subscriber
gets the backlog from its Last-Event-ID first, then live events, so a reconnecting
browser never has a gap.
"""

from __future__ import annotations

import asyncio
import json
from collections import defaultdict

HEARTBEAT_SECONDS = 15


class Broker:
    def __init__(self) -> None:
        self._subscribers: dict[str, set[asyncio.Queue]] = defaultdict(set)

    def subscribe(self, space_id: str) -> asyncio.Queue:
        q: asyncio.Queue = asyncio.Queue(maxsize=256)
        self._subscribers[space_id].add(q)
        return q

    def unsubscribe(self, space_id: str, q: asyncio.Queue) -> None:
        self._subscribers[space_id].discard(q)
        if not self._subscribers[space_id]:
            self._subscribers.pop(space_id, None)

    def publish(self, space_id: str, event: dict) -> None:
        for q in list(self._subscribers.get(space_id, ())):
            try:
                q.put_nowait(event)
            except asyncio.QueueFull:
                # A stalled client is dropped rather than backpressuring a write.
                # It reconnects with Last-Event-ID and replays the backlog.
                self.unsubscribe(space_id, q)


def frame(event: dict) -> str:
    """One SSE message. `event:` is the event type, per openapi streamEvents."""
    return (f"id: {event['seq']}\n"
            f"event: {event['type']}\n"
            f"data: {json.dumps(event, ensure_ascii=False)}\n\n")


async def stream(store, space_id: str, since: int, broker: Broker):
    yield ": connected\n\n"

    last = since
    for event in store.events(space_id, since=since, limit=1000):
        yield frame(event)
        last = event["seq"]

    q = broker.subscribe(space_id)
    try:
        while True:
            try:
                event = await asyncio.wait_for(q.get(), timeout=HEARTBEAT_SECONDS)
            except (TimeoutError, asyncio.TimeoutError):
                yield ": keepalive\n\n"
                continue
            if event["seq"] <= last:
                continue          # already sent as backlog
            last = event["seq"]
            yield frame(event)
    finally:
        broker.unsubscribe(space_id, q)
