"""FastMCP stdio server — SPEC §4.1's ten tools.

A thin proxy over client/. Every rule, including the get_feedback delta, lives in
the server; contracts/README.md is explicit that delta computation is an endpoint so
it cannot drift between the MCP surface and the CLI.

Note the directory name: `mcp_server/`, not `mcp/` as in SPEC §6. A top-level `mcp/`
shadows the `mcp` PyPI package FastMCP imports. See DECISIONS.md.
"""

from __future__ import annotations

import functools
import time
from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from analog.client import Analog, AnalogError, CardDraft

INSTRUCTIONS = """\
A shared canvas you and a human both write to.

Call get_feedback(slug) at the start of every turn: it returns exactly what the
human changed, already diffed. Nothing back means nothing changed.

One idea per card — a wall of text cannot be annotated usefully. Use kind="html"
or kind="svg" for anything visual; the human can pin comments on regions of it.
Always label links. Don't edit or delete the human's cards, don't rearrange the
canvas, and don't resolve annotations you haven't acted on.
"""


def create_server(client: Analog | None = None, *, name: str = "analog") -> FastMCP:
    """`client` is injectable so the tools can be tested against a mock."""
    mcp = FastMCP(name, instructions=INSTRUCTIONS)
    resolved = client

    def api() -> Analog:
        nonlocal resolved
        if resolved is None:
            resolved = Analog()
        return resolved

    def tool(fn):
        """An API failure should reach the agent as a readable message, not a stack."""
        @functools.wraps(fn)
        def wrapper(*args, **kwargs):
            try:
                return fn(*args, **kwargs)
            except AnalogError as exc:
                raise ToolError(str(exc)) from exc

        return mcp.tool(wrapper)

    @tool
    def list_spaces() -> list[dict]:
        """List every space, with card / link / open-annotation counts."""
        return api().list_spaces()

    @tool
    def create_space(
        slug: Annotated[str, Field(description="lowercase, digits and dashes")],
        title: str,
        revision_mode: Literal["replace", "branch"] = "replace",
    ) -> dict:
        """Create a space. `branch` keeps superseded cards visible."""
        return api().create_space(slug, title, revision_mode)

    @tool
    def read_space(slug: str) -> dict:
        """The whole space: nodes, edges, and the open annotations on it."""
        a = api()
        canvas = a.get_canvas(slug)
        return {"space": a.get_space(slug),
                "nodes": canvas["nodes"], "edges": canvas["edges"],
                "annotations": a.list_annotations(slug, resolved=False)}

    @tool
    def add_cards(
        slug: str,
        cards: Annotated[list[CardDraft], Field(
            description="{title, content, kind?: md|html|svg|plain, x?, y?}. "
                        "Omit x/y and the server places the card for you.")],
    ) -> list[dict]:
        """Post cards. One idea per card, or the human cannot annotate them."""
        return api().create_cards(slug, cards)

    @tool
    def update_card(
        slug: str, card_id: str,
        patch: Annotated[dict[str, Any], Field(
            description="Any subset of a JSON Canvas node, e.g. {'text': '...'}")],
        mode: Literal["replace", "branch"] | None = None,
        if_match: Annotated[int | None, Field(
            description="The sp_rev you read. Returns a conflict if it moved on.")] = None,
    ) -> dict:
        """Rewrite a card. In branch mode this returns the NEW card."""
        return api().update_card(slug, card_id, patch, mode=mode, if_match=if_match)

    @tool
    def delete_card(slug: str, card_id: str) -> dict:
        """Remove a card. Don't delete cards the human created — annotate instead."""
        api().delete_card(slug, card_id)
        return {"deleted": card_id}

    @tool
    def link_cards(slug: str, from_card: str, to_card: str,
                   label: Annotated[str | None, Field(
                       description="Always label. Unlabelled edges are noise.")] = None,
                   ) -> dict:
        """Draw a labelled edge between two cards."""
        return api().link_cards(slug, from_card, to_card, label)

    @tool
    def get_feedback(
        slug: str,
        since: Annotated[int | None, Field(
            description="Replay from this seq. Normally omit it: the server keeps a "
                        "cursor per actor, so you stay stateless.")] = None,
    ) -> dict:
        """What the human changed since you last looked.

        All unresolved annotations come back every call — they ignore the cursor —
        while card and link deltas are cursor-governed and exclude your own writes.
        `motivation: editing` is an instruction, `assessing` is a verdict,
        `commenting` is context. Deleted cards mean the human rejected that idea.
        """
        return api().get_feedback(slug, since=since)

    @tool
    def resolve_annotation(
        annotation_id: str,
        reply: Annotated[str | None, Field(description="What you did about it")] = None,
        slug: Annotated[str | None, Field(
            description="Optional; without it the annotation is looked up by id")] = None,
    ) -> dict:
        """Mark one annotation done. Don't resolve what you haven't acted on."""
        a = api()
        target = slug or a.find_annotation(annotation_id)[0]
        return a.resolve_annotation(target, annotation_id, reply=reply)

    @tool
    def await_feedback(
        slug: str,
        since: Annotated[int | None, Field(description="Defaults to your cursor")] = None,
        timeout_s: Annotated[float, Field(description="Give up after this long")] = 300.0,
        poll_s: float = 2.0,
    ) -> dict:
        """Block until the human does something, then return it. For resident agents.

        Returns the same shape as get_feedback; `summary` is empty if it timed out.
        """
        a = api()
        deadline = time.monotonic() + timeout_s
        while True:
            peek = a.get_feedback(slug, since=since, advance=False)
            if peek["summary"]:
                return a.get_feedback(slug, since=since)
            if time.monotonic() >= deadline:
                return peek
            time.sleep(min(poll_s, max(0.0, deadline - time.monotonic())))

    return mcp


mcp = create_server()


def main() -> None:
    mcp.run()


if __name__ == "__main__":
    main()
