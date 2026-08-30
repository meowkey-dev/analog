#!/usr/bin/env python3
"""Deprecated: use `analog onboard <actor>` — same flags, no checkout needed.

The script is now a subcommand of the CLI (issue #31). This shim forwards to it and
will be removed in the next minor release. `--bin-dir` still works: it is consumed
here to find the `analog` binary and not passed on.
"""

import os
import shutil
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent


def binary(name: str, bin_dir: Path | None) -> str:
    for candidate in (bin_dir,
                      Path(os.environ["ANALOG_BIN_DIR"])
                      if os.environ.get("ANALOG_BIN_DIR") else None,
                      REPO_ROOT / "bin"):
        if candidate and (candidate.expanduser() / name).is_file():
            return str((candidate.expanduser() / name).resolve())
    found = shutil.which(name)
    if found:
        return found
    raise SystemExit(f"cannot find `{name}`. Build it with scripts/build.sh, "
                     f"or pass --bin-dir.")


def main(argv: list[str]) -> int:
    bin_dir = None
    forwarded = []
    rest = list(argv)
    # The subcommand's --wrapper / --claude-env take a value; the script allowed
    # them bare, so translate the bare forms to the defaults it used.
    defaults = {"--wrapper": "~/.local/bin", "--claude-env": "."}
    while rest:
        arg = rest.pop(0)
        if arg == "--bin-dir":
            bin_dir = Path(rest.pop(0))
        elif arg.startswith("--bin-dir="):
            bin_dir = Path(arg.split("=", 1)[1])
        elif arg in defaults and (not rest or rest[0].startswith("-")):
            forwarded.extend([arg, defaults[arg]])
        else:
            forwarded.append(arg)

    print("scripts/onboard_agent.py is deprecated; use `analog onboard` "
          "(same flags). The script will be removed in the next release.\n",
          file=sys.stderr)
    analog = binary("analog", bin_dir)
    os.execv(analog, [analog, "onboard", *forwarded])


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
