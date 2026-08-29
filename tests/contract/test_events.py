"""WP1: 'every mutation emits exactly one event'.

The event log is the only reason attribution and deltas work, so it gets its own
file rather than being checked incidentally elsewhere.
"""

from __future__ import annotations

import pytest

from tests.conftest import AGENT, HUMAN, add_cards, assert_valid, make_space, one_card

pytestmark = pytest.mark.contract


@pytest.fixture
def space(client):
    make_space(client, "demo")
    return client


def events(client, since=0):
    return client.get("/api/spaces/demo/events", params={"since": since}).json()["events"]


def count(client):
    return len(events(client))


# --- exactly one event per mutation -----------------------------------------

def test_each_mutation_emits_exactly_one_event(space):
    client = space
    base = count(client)
    assert base == 1, "the space's own creation"

    card = one_card(client, "demo", title="A")
    assert count(client) == base + 1

    other = one_card(client, "demo", title="B")
    assert count(client) == base + 2

    client.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"x": 5})
    assert count(client) == base + 3

    client.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"text": "v2"})
    assert count(client) == base + 4

    link = client.post("/api/spaces/demo/links", params=AGENT, json={
        "edges": [{"fromNode": card["id"], "toNode": other["id"]}]}).json()[0]
    assert count(client) == base + 5

    ann = client.post("/api/spaces/demo/annotations", params=HUMAN, json={
        "card_id": card["id"], "body": "b"}).json()
    assert count(client) == base + 6

    client.patch(f"/api/spaces/demo/annotations/{ann['id']}", params=AGENT,
                 json={"resolved": True})
    assert count(client) == base + 7

    client.delete(f"/api/spaces/demo/links/{link['id']}", params=HUMAN)
    assert count(client) == base + 8

    client.delete(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN)
    assert count(client) == base + 9

    assert [e["type"] for e in events(client)] == [
        "space.created",
        "card.created", "card.created", "card.moved", "card.updated", "link.created",
        "annotation.created", "annotation.resolved", "link.deleted", "card.deleted"]


def test_bulk_create_emits_one_event_per_item(space):
    add_cards(space, "demo", [{"title": t, "content": t} for t in "ABCD"])
    created = [e for e in events(space) if e["type"] == "card.created"]
    assert len(created) == 4


def test_a_failed_mutation_emits_nothing(space):
    card = one_card(space, "demo")
    before = count(space)
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                headers={"If-Match": "99"}, json={"text": "x"})
    space.patch("/api/spaces/demo/cards/c_nope", params=HUMAN, json={"text": "x"})
    space.post("/api/spaces/demo/cards", params={"actor": "x"},
               json={"cards": [{"title": "T", "content": "c"}]})
    assert count(space) == before


# --- shape -------------------------------------------------------------------

def test_events_validate_and_carry_attribution(space):
    one_card(space, "demo")
    ev = next(e for e in events(space) if e["type"] == "card.created")
    assert_valid(ev, "Event")
    assert ev["actor"] == "claude-code" and ev["actor_kind"] == "agent"
    assert ev["ts"].endswith("Z")


def test_seq_starts_at_one_and_is_contiguous(space):
    add_cards(space, "demo", [{"title": t, "content": t} for t in "ABC"])
    log = events(space)
    assert [e["seq"] for e in log] == [1, 2, 3, 4]
    assert log[0]["type"] == "space.created"


def test_subject_id_points_at_the_thing_that_changed(space):
    card = one_card(space, "demo")
    ann = space.post("/api/spaces/demo/annotations", params=HUMAN,
                     json={"card_id": card["id"], "body": "b"}).json()
    by_type = {e["type"]: e for e in events(space)}
    assert by_type["card.created"]["subject_id"] == card["id"]
    assert by_type["annotation.created"]["subject_id"] == ann["id"]
    assert by_type["annotation.created"]["payload"]["card_id"] == card["id"]


def test_card_created_payload_carries_the_title(space):
    """The activity sidebar and cards_deleted both need a title without the card."""
    one_card(space, "demo", title="Option D")
    created = next(e for e in events(space) if e["type"] == "card.created")
    assert created["payload"]["title"] == "Option D"


def test_link_created_payload_carries_endpoints_and_label(space):
    a, b = add_cards(space, "demo", [{"title": "A", "content": "a"},
                                     {"title": "B", "content": "b"}])
    space.post("/api/spaces/demo/links", params=HUMAN, json={
        "edges": [{"fromNode": a["id"], "toNode": b["id"], "label": "depends on"}]})
    payload = events(space)[-1]["payload"]
    assert payload == {"from": a["id"], "to": b["id"], "label": "depends on"}


# --- listing -----------------------------------------------------------------

def test_since_is_exclusive(space):
    add_cards(space, "demo", [{"title": t, "content": t} for t in "ABC"])
    assert [e["seq"] for e in events(space, since=2)] == [3, 4]
    assert events(space, since=4) == []


def test_limit_and_cursor(space):
    add_cards(space, "demo", [{"title": str(i), "content": "c"} for i in range(5)])
    r = space.get("/api/spaces/demo/events", params={"since": 0, "limit": 2}).json()
    assert [e["seq"] for e in r["events"]] == [1, 2]
    assert r["cursor"] == 2, "the cursor is resumable: pass it back as `since`"

    rest = space.get("/api/spaces/demo/events", params={"since": r["cursor"]}).json()
    assert [e["seq"] for e in rest["events"]] == [3, 4, 5, 6]


def test_cursor_when_nothing_is_returned(space):
    r = space.get("/api/spaces/demo/events", params={"since": 7}).json()
    assert r["events"] == [] and r["cursor"] == 7


def test_events_for_an_unknown_space_is_404(client):
    assert client.get("/api/spaces/nope/events").status_code == 404


# --- SSE (openapi streamEvents) ---------------------------------------------

def _frames(lines, want: int, timeout_at):
    """Collect `want` SSE frames from an iterator of lines."""
    import json as _json
    import time

    out, current = [], {}
    for line in lines:
        if time.monotonic() > timeout_at:
            break
        if line.startswith("id:"):
            current["id"] = int(line[3:].strip())
        elif line.startswith("event:"):
            current["event"] = line[6:].strip()
        elif line.startswith("data:"):
            current["data"] = _json.loads(line[5:].strip())
        elif line == "":
            if current.get("data"):
                out.append(current)
                if len(out) >= want:
                    return out
            current = {}
    return out


def test_stream_replays_the_backlog_then_pushes_live_events(live_server):
    import threading
    import time

    import httpx

    base = live_server
    httpx.post(f"{base}/api/spaces", params=HUMAN, json={"slug": "demo", "title": "D"})
    card = httpx.post(f"{base}/api/spaces/demo/cards", params=AGENT,
                      json={"cards": [{"title": "A", "content": "a"}]}).json()[0]

    with httpx.stream("GET", f"{base}/api/spaces/demo/events/stream",
                      headers={"Last-Event-ID": "0"}, timeout=15) as r:
        assert r.status_code == 200
        assert r.headers["content-type"].startswith("text/event-stream")
        assert r.headers["cache-control"] == "no-cache"

        lines = r.iter_lines()
        backlog = _frames(lines, 2, time.monotonic() + 10)
        assert [f["event"] for f in backlog] == ["space.created", "card.created"]
        assert [f["id"] for f in backlog] == [1, 2]
        assert backlog[1]["data"]["subject_id"] == card["id"]

        def later():
            time.sleep(0.3)
            httpx.patch(f"{base}/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                        json={"text": "v2"})

        threading.Thread(target=later, daemon=True).start()
        live = _frames(lines, 1, time.monotonic() + 10)
        assert [f["event"] for f in live] == ["card.updated"]
        assert live[0]["id"] == 3
        assert live[0]["data"]["actor"] == "human"


def test_stream_resumes_from_last_event_id(live_server):
    import time

    import httpx

    base = live_server
    httpx.post(f"{base}/api/spaces", params=HUMAN, json={"slug": "demo", "title": "D"})
    httpx.post(f"{base}/api/spaces/demo/cards", params=AGENT,
               json={"cards": [{"title": t, "content": t} for t in "ABC"]})

    with httpx.stream("GET", f"{base}/api/spaces/demo/events/stream",
                      headers={"Last-Event-ID": "3"}, timeout=15) as r:
        frames = _frames(r.iter_lines(), 1, time.monotonic() + 10)
        assert [f["id"] for f in frames] == [4], "events 1-3 were already delivered"
