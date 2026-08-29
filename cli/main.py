"""`analog` — SPEC §4.2.

A thin shell over client/. No rules live here: what a command prints is a
presentation choice, what it means is the server's.
"""

from __future__ import annotations

import json
import sys
import time
import webbrowser
from pathlib import Path
from typing import Annotated, Any, Optional

import typer

from client import Analog, AnalogError, Conflict

app = typer.Typer(add_completion=False, no_args_is_help=True,
                  help="A shared canvas for you and your agents.")

STDIN = "-"


def err(message: str) -> None:
    print(message, file=sys.stderr)


def api() -> Analog:
    return Analog()


def read_source(source: str | None, file: str | None) -> str:
    """`-` means stdin (SPEC §4.2), so agents can pipe generated content in."""
    target = file or source
    if target is None:
        raise typer.BadParameter("provide a file path, or - to read stdin")
    if target == STDIN:
        return sys.stdin.read()
    path = Path(target)
    if not path.is_file():
        raise typer.BadParameter(f"no such file: {target}")
    return path.read_text()


def out(value: Any) -> None:
    print(json.dumps(value, indent=2, ensure_ascii=False))


def run(fn):
    """Exit non-zero with a message on stderr, so agents notice failures."""
    try:
        return fn()
    except Conflict as exc:
        err(f"conflict: {exc.body.get('message', '')}")
        current = exc.current
        if current:
            err(f"  current sp_rev is {current.get('sp_rev')}")
        raise typer.Exit(2)
    except AnalogError as exc:
        err(str(exc))
        raise typer.Exit(1)
    except typer.Exit:
        raise
    except (BrokenPipeError, KeyboardInterrupt):
        raise typer.Exit(130)


# --- spaces ------------------------------------------------------------------

@app.command()
def spaces(json_out: Annotated[bool, typer.Option("--json")] = False):
    """List spaces."""
    rows = run(lambda: api().list_spaces())
    if json_out:
        return out(rows)
    if not rows:
        return
    width = max(len(s["slug"]) for s in rows)
    for space in rows:
        counts = space.get("counts") or {}
        print(f"{space['slug']:<{width}}  {space['title']}"
              f"  [{counts.get('cards', 0)} cards, {counts.get('links', 0)} links,"
              f" {counts.get('open_annotations', 0)} open]")


@app.command()
def new(slug: str,
        title: Annotated[Optional[str], typer.Option("--title")] = None,
        mode: Annotated[str, typer.Option("--mode", help="replace | branch")] = "replace"):
    """Create a space."""
    space = run(lambda: api().create_space(slug, title or slug, mode))
    print(f"{space['slug']}  {api().space_url(slug)}")


def open_(slug: str, browser: Annotated[bool, typer.Option("--browser")] = False):
    """Print the URL for a space; --browser to launch it."""
    url = api().space_url(slug)
    run(lambda: api().get_space(slug))
    print(url)
    if browser:
        webbrowser.open(url)


app.command("open")(open_)


@app.command()
def rm_space(slug: str, yes: Annotated[bool, typer.Option("--yes")] = False):
    """Delete a space and everything in it."""
    if not yes:
        err(f"refusing to delete {slug!r} without --yes")
        raise typer.Exit(1)
    run(lambda: api().delete_space(slug))


# --- feedback (the important one) -------------------------------------------

def render_feedback(feedback: dict) -> None:
    if not feedback["summary"]:
        return                      # SPEC §4.2: silence means nothing changed
    print(feedback["summary"])

    if feedback["annotations"]:
        print("\ncomments")
        for a in feedback["annotations"]:
            flags = " (stale)" if a["stale"] else ""
            print(f"  {a['id']}  [{a['motivation']}] {a.get('card_title') or a['card_id']}"
                  f"{flags}  · {a['creator']}")
            for line in a["body"].splitlines() or [""]:
                print(f"      {line}")
            print(f"      resolve: analog resolve {a['id']} --reply \"...\"")

    for key, heading in (("cards_edited", "cards edited"),
                         ("cards_deleted", "cards deleted"),
                         ("cards_moved", "cards moved")):
        if feedback[key]:
            print(f"\n{heading}")
            for c in feedback[key]:
                changed = f"  ({', '.join(c['changed'])})" if c.get("changed") else ""
                print(f"  {c['id']}  {c.get('title') or ''}{changed}  · {c.get('actor')}")

    if feedback["links_added"]:
        print("\nlinks added")
        for l in feedback["links_added"]:
            label = f'  "{l["label"]}"' if l.get("label") else ""
            print(f"  {l['id']}  {l['from']} -> {l['to']}{label}  · {l.get('actor')}")
    if feedback["links_removed"]:
        print("\nlinks removed")
        for l in feedback["links_removed"]:
            print(f"  {l['id']}  · {l.get('actor')}")


@app.command()
def feedback(slug: str,
             json_out: Annotated[bool, typer.Option("--json")] = False,
             watch: Annotated[bool, typer.Option("--watch")] = False,
             since: Annotated[Optional[int], typer.Option("--since")] = None,
             peek: Annotated[bool, typer.Option("--peek",
                                                help="do not advance the cursor")] = False):
    """What changed since this actor last looked. Prints nothing if nothing did."""
    a = api()
    first = run(lambda: a.get_feedback(slug, since=since, advance=not peek))
    out(first) if json_out else render_feedback(first)
    if not watch:
        return

    cursor = first["cursor"]
    for event in a.stream_events(slug, since=cursor):
        if event.get("actor") == a.actor:
            continue
        time.sleep(0.2)                       # coalesce a burst into one report
        delta = a.get_feedback(slug, advance=not peek)
        if delta["summary"]:
            out(delta) if json_out else render_feedback(delta)
            sys.stdout.flush()


# --- cards -------------------------------------------------------------------

@app.command()
def add(slug: str,
        source: Annotated[Optional[str], typer.Argument(help="file, or - for stdin")] = None,
        title: Annotated[str, typer.Option("--title")] = "",
        kind: Annotated[str, typer.Option("--kind", help="md | html | svg | plain")] = "md",
        file: Annotated[Optional[str], typer.Option("--file")] = None,
        text: Annotated[Optional[str], typer.Option("--text", help="inline content")] = None,
        json_out: Annotated[bool, typer.Option("--json")] = False):
    """Post a card."""
    content = text if text is not None else read_source(source, file)
    node = run(lambda: api().create_cards(
        slug, [{"title": title, "content": content, "kind": kind}])[0])
    out(node) if json_out else print(f"{node['id']}  {node.get('sp_title', '')}")


@app.command()
def cards(slug: str, json_out: Annotated[bool, typer.Option("--json")] = False,
          deleted: Annotated[bool, typer.Option("--deleted")] = False):
    """List cards: id, title, kind, created_by."""
    canvas = run(lambda: api().get_canvas(slug, include_deleted=deleted))
    if json_out:
        return out(canvas["nodes"])
    for n in canvas["nodes"]:
        kind = n.get("sp_kind") or n["type"]
        flags = []
        if n.get("sp_deleted_at"):
            flags.append("deleted")
        if n.get("sp_superseded_by"):
            flags.append(f"superseded by {n['sp_superseded_by']}")
        suffix = f"  ({'; '.join(flags)})" if flags else ""
        print(f"{n['id']}  {n.get('sp_title', ''):<28}  {kind:<5}  "
              f"{n.get('sp_created_by', '')}  rev {n.get('sp_rev', 1)}{suffix}")


@app.command()
def update(slug: str, card_id: str,
           source: Annotated[Optional[str], typer.Argument()] = None,
           file: Annotated[Optional[str], typer.Option("--file")] = None,
           text: Annotated[Optional[str], typer.Option("--text")] = None,
           title: Annotated[Optional[str], typer.Option("--title")] = None,
           mode: Annotated[Optional[str], typer.Option("--mode",
                                                       help="replace | branch")] = None,
           if_match: Annotated[Optional[int], typer.Option("--if-match")] = None,
           json_out: Annotated[bool, typer.Option("--json")] = False):
    """Replace a card's content."""
    patch: dict[str, Any] = {}
    if text is not None or source or file:
        patch["text"] = text if text is not None else read_source(source, file)
    if title is not None:
        patch["sp_title"] = title
    if not patch:
        raise typer.BadParameter("nothing to update: pass a file, --text or --title")
    node = run(lambda: api().update_card(slug, card_id, patch, mode=mode, if_match=if_match))
    out(node) if json_out else print(f"{node['id']}  rev {node.get('sp_rev')}")


@app.command()
def rm(slug: str, card_id: str):
    """Delete a card (soft; the agent still sees that you removed it)."""
    run(lambda: api().delete_card(slug, card_id))


@app.command()
def link(slug: str, from_id: str, to_id: str,
         label: Annotated[Optional[str], typer.Option("--label")] = None,
         json_out: Annotated[bool, typer.Option("--json")] = False):
    """Link two cards. Always label: unlabelled edges are noise."""
    edge = run(lambda: api().link_cards(slug, from_id, to_id, label))
    out(edge) if json_out else print(f"{edge['id']}  {from_id} -> {to_id}"
                                     f"{'  ' + repr(label) if label else ''}")


@app.command()
def unlink(slug: str, link_id: str):
    """Remove a link."""
    run(lambda: api().delete_link(slug, link_id))


# --- annotations -------------------------------------------------------------

@app.command()
def comments(slug: str, json_out: Annotated[bool, typer.Option("--json")] = False,
             all_: Annotated[bool, typer.Option("--all",
                                                help="include resolved")] = False):
    """List annotations on a space."""
    rows = run(lambda: api().list_annotations(slug, resolved=None if all_ else False))
    if json_out:
        return out(rows)
    for a in rows:
        marks = "".join(["*" if a["resolved"] else " ", "~" if a["stale"] else " "])
        print(f"{a['id']} {marks} [{a['motivation']}] "
              f"{a.get('card_title') or a['card_id']}: {a['body']}")


@app.command()
def resolve(annotation_id: str,
            reply: Annotated[Optional[str], typer.Option("--reply")] = None,
            slug: Annotated[Optional[str], typer.Option("--space")] = None):
    """Mark an annotation done. Don't resolve what you haven't acted on."""
    a = api()

    def go():
        target = slug or a.config_space
        if target:
            return target, a.resolve_annotation(target, annotation_id, reply=reply)
        found, _ = a.find_annotation(annotation_id)
        return found, a.resolve_annotation(found, annotation_id, reply=reply)

    found, annotation = run(go)
    print(f"resolved {annotation['id']} in {found}")


# --- export / import ---------------------------------------------------------

@app.command()
def export(slug: str,
           deleted: Annotated[bool, typer.Option("--deleted")] = False):
    """Write the space as JSON Canvas on stdout. Opens in Obsidian."""
    out(run(lambda: api().get_canvas(slug, include_deleted=deleted)))


@app.command("import")
def import_(slug: str,
            source: Annotated[Optional[str], typer.Argument()] = None,
            file: Annotated[Optional[str], typer.Option("--file")] = None,
            json_out: Annotated[bool, typer.Option("--json")] = False):
    """Merge a JSON Canvas file into a space. Additive: never deletes."""
    raw = read_source(source or STDIN, file)
    result = run(lambda: api().import_canvas(slug, json.loads(raw)))
    if json_out:
        return out(result)
    canvas = result["canvas"]
    print(f"imported {len(canvas['nodes'])} cards, {len(canvas['edges'])} links")


@app.command()
def upload(slug: str, path: str,
           title: Annotated[str, typer.Option("--title")] = "",
           json_out: Annotated[bool, typer.Option("--json")] = False):
    """Upload an image and place it as a JSON Canvas file node."""
    a = api()
    media = run(lambda: a.upload_media(slug, path))
    node = run(lambda: a.create_nodes(slug, [{
        "type": "file", "file": media["url"], "sp_title": title or Path(path).name,
        "width": 360, "height": 280}])[0])
    out(node) if json_out else print(f"{node['id']}  {node['file']}")


@app.command()
def events(slug: str, since: Annotated[int, typer.Option("--since")] = 0,
           json_out: Annotated[bool, typer.Option("--json")] = False,
           watch: Annotated[bool, typer.Option("--watch")] = False):
    """Raw event log. Mostly for debugging the cursor."""
    a = api()
    page = run(lambda: a.list_events(slug, since=since))
    if json_out:
        out(page)
    else:
        for e in page["events"]:
            print(f"{e['seq']:>4}  {e['ts']}  {e['type']:<19} {e['subject_id']:<28}"
                  f" {e['actor']}")
    if watch:
        for event in a.stream_events(slug, since=page["cursor"]):
            if json_out:
                out(event)
            else:
                print(f"{event['seq']:>4}  {event['ts']}  {event['type']:<19}"
                      f" {event['subject_id']:<28} {event['actor']}")
            sys.stdout.flush()


def main() -> None:
    app()


if __name__ == "__main__":
    main()
