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

from client import Analog, AnalogError, Conflict, Unauthorized

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
    except Unauthorized as exc:
        err(f"unauthorized: {exc.body.get('message', '')}")
        err("  set ANALOG_TOKEN, or run `analog login <url> --token ...`")
        raise typer.Exit(3)
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


# --- connection --------------------------------------------------------------

@app.command()
def whoami(json_out: Annotated[bool, typer.Option("--json")] = False):
    """Which server this shell talks to, and who it writes as."""
    a = api()
    health = run(lambda: a.health())
    identity = run(lambda: a.whoami()) if health.get("auth_required") else {
        "authenticated": False, "actor": a.actor, "actor_kind": a.actor_kind}
    payload = {"url": a.base, "configured_actor": a.actor, **health, **identity}
    if json_out:
        return out(payload)
    print(f"server  {a.base}  (analog {health.get('version', '?')})")
    print(f"auth    {'per-actor tokens' if health.get('auth_required') else 'off'}")
    if health.get("auth_required"):
        print(f"token   {'valid' if identity.get('authenticated') else 'MISSING OR INVALID'}")
        if identity.get("authenticated") and identity["actor"] != a.actor:
            print(f"        warning: ANALOG_ACTOR is {a.actor!r} but this token "
                  f"writes as {identity['actor']!r}; writes will be refused")
    print(f"actor   {identity.get('actor') or a.actor}"
          f" ({identity.get('actor_kind') or a.actor_kind})")


@app.command()
def login(url: str,
          token: Annotated[Optional[str], typer.Option("--token")] = None,
          actor: Annotated[Optional[str], typer.Option("--actor")] = None,
          kind: Annotated[str, typer.Option("--kind")] = "agent",
          path: Annotated[Optional[str], typer.Option("--config")] = None):
    """Remember a server in ~/.analog.toml so ANALOG_* need not be set every time."""
    import os

    target = Path(path or os.environ.get("ANALOG_CONFIG", Path.home() / ".analog.toml"))
    probe = Analog(url=url, actor=actor or "probe", token=token, config={})
    health = run(lambda: probe.health())
    if health.get("auth_required") and not token:
        err("this server requires a token; pass --token")
        raise typer.Exit(1)
    if token:
        try:
            identity = probe.whoami()
        except Unauthorized:
            err(f"{probe.base} did not accept that token")
            raise typer.Exit(3)
        if not identity.get("authenticated"):
            err(f"{probe.base} did not accept that token")
            raise typer.Exit(3)
        actor, kind = identity["actor"], identity["actor_kind"]

    lines = [f'url = "{probe.base.removesuffix("/api")}"']
    if actor:
        lines += [f'actor = "{actor}"', f'kind = "{kind}"'.replace("kind", "actor_kind")]
    if token:
        lines.append(f'token = "{token}"')
    target.write_text("# written by `analog login`\n" + "\n".join(lines) + "\n")
    try:
        target.chmod(0o600)          # it holds a credential
    except OSError:
        pass
    print(f"wrote {target}")
    print(f"  server {probe.base}")
    if actor:
        print(f"  actor  {actor} ({kind})")


# --- tokens (run these on the machine hosting the server) --------------------

tokens_app = typer.Typer(no_args_is_help=True,
                         help="Issue and revoke per-actor tokens. Reads and writes the "
                              "server's auth file, so run it on the server host.")
app.add_typer(tokens_app, name="token")


def _token_store():
    from server import config as server_config
    from server.auth import TokenStore

    return TokenStore(server_config.auth_path())


@tokens_app.command("add")
def token_add(actor: str,
              kind: Annotated[str, typer.Option("--kind", help="human | agent")] = "agent"):
    """Mint a token for one actor. It is shown once and only stored as a digest."""
    from server.auth import AuthError

    store = _token_store()
    try:
        secret = store.issue(actor, kind)
    except AuthError as exc:
        err(str(exc))
        raise typer.Exit(1)
    print(f"{actor} ({kind})")
    print(f"  {secret}")
    print()
    print("Copy it now — it is not recoverable. On the client:")
    print(f"  export ANALOG_ACTOR={actor}")
    print(f"  export ANALOG_TOKEN={secret}")
    print(f"stored in {store.path}")


@tokens_app.command("list")
def token_list(json_out: Annotated[bool, typer.Option("--json")] = False):
    """Every actor with a token. Secrets are not recoverable."""
    store = _token_store()
    entries = store.entries()
    if json_out:
        return out(entries)
    if not entries:
        print(f"no tokens; auth is off ({store.path})")
        return
    for entry in entries:
        print(f"{entry['name']:<20} {entry['kind']:<6} issued {entry.get('created_at', '?')}")


@tokens_app.command("revoke")
def token_revoke(actor: str):
    """Invalidate an actor's token."""
    store = _token_store()
    if not store.revoke(actor):
        err(f"no token for {actor!r}")
        raise typer.Exit(1)
    print(f"revoked {actor}")
    if not store.enabled:
        err("warning: that was the last token — auth is now OFF on this server")


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
