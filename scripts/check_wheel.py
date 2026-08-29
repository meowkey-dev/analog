#!/usr/bin/env python3
"""Install the built wheel somewhere clean and prove it actually works.

A wheel that imports is not a wheel that runs. This starts the installed server
outside the source tree and checks the two things a source checkout hides: that
`schema.sql` came along, and that the UI is served rather than 404ing.
"""

from __future__ import annotations

import http.client
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
PORT = 8799                       # not 8787: don't collide with a dev server


def get(path: str) -> tuple[int, bytes]:
    c = http.client.HTTPConnection("127.0.0.1", PORT, timeout=5)
    c.request("GET", path)
    r = c.getresponse()
    return r.status, r.read()


def main() -> int:
    wheels = sorted((ROOT / "dist").glob("*.whl"))
    if not wheels:
        sys.exit("no wheel in dist/ — run scripts/build_dist.py first")
    wheel = wheels[-1]

    with tempfile.TemporaryDirectory() as tmp:
        tmp = Path(tmp)
        venv = tmp / "venv"
        subprocess.run(["uv", "venv", "-q", str(venv)], check=True)
        subprocess.run(["uv", "pip", "install", "-q", "--python",
                        str(venv / "bin" / "python"), str(wheel)], check=True)

        proc = subprocess.Popen(
            [str(venv / "bin" / "analog-server"), "--port", str(PORT)],
            cwd=tmp, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        try:
            for _ in range(100):
                try:
                    if get("/api/health")[0] == 200:
                        break
                except OSError:
                    time.sleep(0.1)
            else:
                sys.exit(f"server never came up:\n{proc.communicate(timeout=5)[0]}")

            checks = []
            status, body = get("/api/health")
            checks.append(("health", status == 200 and b'"ok":true' in body, status))

            # The one a source checkout cannot tell you: is the SPA packaged?
            status, body = get("/")
            checks.append(("web UI at /", status == 200 and b"<div id=" in body, status))

            status, _ = get("/api/spaces")
            checks.append(("spaces (schema.sql applied)", status == 200, status))

            for name, ok, status in checks:
                print(f"  {'ok  ' if ok else 'FAIL'} {name} (HTTP {status})")
            failed = [n for n, ok, _ in checks if not ok]
            if failed:
                sys.exit(f"\n{wheel.name} is not shippable: {', '.join(failed)}")
            print(f"\n{wheel.name} installs clean and serves.")
        finally:
            proc.terminate()
            proc.wait(timeout=10)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
