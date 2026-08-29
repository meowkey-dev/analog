#!/usr/bin/env python3
"""Give an agent everything it needs to use an Analog server.

Three things have to line up before an agent is useful here: a token (the server
decides who it is), the MCP server or the CLI wired to that token, and the skill,
which teaches the workflow the API cannot — read feedback first, one idea per card,
never resolve what you did not act on.

    # on the machine running the server: mint the token
    python scripts/onboard_agent.py claude-code --issue

    # anywhere the agent runs: install the skill, print the wiring
    python scripts/onboard_agent.py claude-code \\
        --url https://analog.example.com --token analog_... \\
        --skill-into ~/.claude/skills --print-mcp

`--issue` needs the server's auth file, so it only works on the server host. The
rest works anywhere.
"""

from __future__ import annotations

import argparse
import os
import shutil
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(REPO_ROOT))

SKILL_SRC = REPO_ROOT / "skill" / "analog"


def issue(actor: str, kind: str) -> str:
    from server import config
    from server.auth import TokenStore

    store = TokenStore(config.auth_path())
    token = store.issue(actor, kind)
    print(f"issued a token for {actor} ({kind})")
    print(f"  store: {store.path}")
    print("  it is shown once and stored only as a digest.\n")
    return token


def write_wrapper(into: Path, actor: str, kind: str, url: str,
                  token: str | None) -> Path:
    """A command that is already configured as one actor.

    MCP config and skills are read when a session starts, so neither reaches an
    agent that is mid-session. A wrapper on disk does, and it also sidesteps the
    trap in `analog login`: that writes ~/.analog.toml for the *user*, so an agent
    running as you would inherit your identity and write under your name.
    """
    into.mkdir(parents=True, exist_ok=True)
    path = into / f"analog-{actor}"
    analog = Path(sys.executable).parent / "analog"
    lines = [
        "#!/bin/sh",
        f"# Analog, pre-configured as {actor} ({kind}).",
        "# Written by scripts/onboard_agent.py. Contains a token: keep it mode 700",
        "# and out of any repository.",
        f"export ANALOG_URL={url}",
        f"export ANALOG_ACTOR={actor}",
        f"export ANALOG_ACTOR_KIND={kind}",
        *([f"export ANALOG_TOKEN={token}"] if token else []),
        "# Ignore any ~/.analog.toml, which may belong to a different actor.",
        "export ANALOG_CONFIG=/nonexistent",
        f'exec "{analog}" "$@"',
    ]
    path.write_text("\n".join(lines) + "\n")
    path.chmod(0o700)
    return path


def write_claude_env(project: Path, actor: str, kind: str, url: str,
                     token: str | None) -> Path:
    """Merge the ANALOG_* env into a project's .claude/settings.local.json.

    `settings.local.json` rather than `settings.json` because it holds a token and
    is the gitignored one. Claude Code applies `env` to its Bash tool calls, so the
    skill's plain `analog ...` commands work with no wrapper and no exports —
    but it is read at session start, so an already-running agent needs a restart.
    """
    import json

    target = project.expanduser() / ".claude" / "settings.local.json"
    target.parent.mkdir(parents=True, exist_ok=True)

    settings = {}
    if target.is_file() and target.read_text().strip():
        settings = json.loads(target.read_text())      # merge, never clobber

    env = dict(settings.get("env") or {})
    env.update({
        "ANALOG_URL": url,
        "ANALOG_ACTOR": actor,
        "ANALOG_ACTOR_KIND": kind,
        # Otherwise a ~/.analog.toml belonging to a different actor wins.
        "ANALOG_CONFIG": "/nonexistent",
    })
    if token:
        env["ANALOG_TOKEN"] = token
    settings["env"] = env

    target.write_text(json.dumps(settings, indent=2) + "\n")
    return target


def install_skill(into: Path) -> Path:
    """Skills are a folder with a SKILL.md; copying is the whole install."""
    target = into.expanduser() / "analog"
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.exists():
        shutil.rmtree(target)
    shutil.copytree(SKILL_SRC, target)
    return target


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("actor", help="the agent's name, e.g. claude-code")
    ap.add_argument("--kind", default="agent", choices=["agent", "human"])
    ap.add_argument("--issue", action="store_true",
                    help="mint a token (server host only)")
    ap.add_argument("--url", default="http://127.0.0.1:8787")
    ap.add_argument("--token", default=None,
                    help="an existing token; implied by --issue")
    ap.add_argument("--skill-into", type=Path, default=None, metavar="DIR",
                    help="copy the skill here, e.g. ~/.claude/skills or "
                         "<project>/.claude/skills")
    ap.add_argument("--print-mcp", action="store_true",
                    help="print the `claude mcp add` command")
    ap.add_argument("--print-env", action="store_true",
                    help="print the exports an agent with only a shell needs")
    ap.add_argument("--claude-env", nargs="?", const=".", default=None, metavar="PROJECT",
                    help="merge ANALOG_* into PROJECT/.claude/settings.local.json, so "
                         "the skill's plain `analog` commands work in that project")
    ap.add_argument("--wrapper", nargs="?", const="~/.local/bin", default=None,
                    metavar="DIR",
                    help="write a wrapper command carrying this actor's config, for "
                         "an agent that is already running and cannot pick up new "
                         "MCP config or skills without a restart")
    args = ap.parse_args(argv)

    token = args.token
    if args.issue:
        token = issue(args.actor, args.kind)

    if args.skill_into:
        target = install_skill(args.skill_into)
        print(f"skill installed: {target}")
        print("  it loads on demand, so it costs nothing in unrelated sessions.\n")

    if args.claude_env is not None:
        target = write_claude_env(Path(args.claude_env), args.actor, args.kind,
                                  args.url, token)
        print(f"claude env: {target}")
        if not token:
            print("  no token written — add ANALOG_TOKEN there once the server issues one.")
        print("  Read at session start, so restart the agent for it to take effect.\n")

    python = Path(sys.executable)
    if args.wrapper is not None:
        path = write_wrapper(Path(args.wrapper).expanduser(), args.actor, args.kind,
                             args.url, token)
        print(f"wrapper: {path}")
        print(f"  A running agent can use it immediately — no restart, no exports:\n"
              f"    {path.name} whoami\n"
              f"    {path.name} feedback <slug>\n")
        print("  It carries the token, so it is mode 700 and lives outside the repo.")
        if str(path.parent) not in os.environ.get("PATH", "").split(os.pathsep):
            print(f"  {path.parent} is not on PATH; use the full path or add it.\n")
        else:
            print()

    mcp_entry = python.parent / "analog-mcp"
    command = str(mcp_entry) if mcp_entry.exists() else (
        f"{python} {REPO_ROOT / 'mcp_server' / 'server.py'}")

    if args.print_mcp:
        secret = token or "$ANALOG_TOKEN"
        print("wire up MCP (stdio) — run this where the agent runs:\n")
        print(f"  claude mcp add analog \\")
        print(f"    -e ANALOG_URL={args.url} \\")
        print(f"    -e ANALOG_ACTOR={args.actor} \\")
        print(f"    -e ANALOG_ACTOR_KIND={args.kind} \\")
        print(f"    -e ANALOG_TOKEN={secret} \\")
        print(f"    -- {command}")
        print()
        print("  --scope user puts it in every project; the default is this one only.")
        print("  Check it with:  claude mcp get analog\n")

    if args.print_env or not (args.print_mcp or args.skill_into):
        print("or, for an agent that only has a shell:\n")
        print(f"  export ANALOG_URL={args.url}")
        print(f"  export ANALOG_ACTOR={args.actor}")
        print(f"  export ANALOG_ACTOR_KIND={args.kind}")
        print(f"  export ANALOG_TOKEN={token or '<token>'}")
        print(f"  export PATH={python.parent}:$PATH")
        print("\n  then:  analog whoami   # confirms who the server thinks you are")

    if token and not args.print_mcp and not args.print_env:
        print(f"token: {token}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
