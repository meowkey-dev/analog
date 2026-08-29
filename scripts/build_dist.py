#!/usr/bin/env python3
"""Build the wheel and sdist, with the web UI inside them.

`pip install analog` should give you the whole thing, not an API with no page
behind it — so the built SPA is copied into `analog/server/web/` and declared as
package data. It is build output, so it is gitignored and rebuilt here rather than
committed.

    python scripts/build_dist.py            # -> dist/
    python scripts/build_dist.py --check    # ...then verify the wheel serves a UI
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SRC = ROOT / "web" / "dist"
PACKAGED = ROOT / "analog" / "server" / "web"


def build_web() -> None:
    if not (SRC / "index.html").is_file():
        print("building the web bundle first")
        subprocess.run(["npm", "ci"], cwd=ROOT / "web", check=True)
        subprocess.run(["npm", "run", "build"], cwd=ROOT / "web", check=True)


def stage() -> None:
    if PACKAGED.exists():
        shutil.rmtree(PACKAGED)
    # No .map files: they are 2MB of the 2.5MB bundle and nobody debugging a
    # released build has the sources to match them against.
    shutil.copytree(SRC, PACKAGED, ignore=shutil.ignore_patterns("*.map"))
    print(f"staged {sum(1 for _ in PACKAGED.rglob('*') if _.is_file())} files "
          f"into {PACKAGED.relative_to(ROOT)}")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--check", action="store_true",
                    help="install the wheel in a throwaway venv and confirm it serves")
    args = ap.parse_args()

    build_web()
    stage()
    subprocess.run(["uv", "build", "--out-dir", str(ROOT / "dist")], cwd=ROOT, check=True)

    if args.check:
        subprocess.run([sys.executable, str(ROOT / "scripts" / "check_wheel.py")],
                       check=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
