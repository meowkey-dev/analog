"""`analog-server` — run the API.

Exists so the "don't serve an unauthenticated canvas to a network" check runs before
uvicorn binds, rather than after. `uvicorn server.main:app` still works and hits the
same check inside create_app().
"""

from __future__ import annotations

import argparse
import sys

from server import config
from server.auth import AuthError, TokenStore, require_auth_for_host


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="analog-server", description=__doc__)
    parser.add_argument("--host", default=config.HOST,
                        help="0.0.0.0 to accept connections from other machines")
    parser.add_argument("--port", type=int, default=config.PORT)
    parser.add_argument("--reload", action="store_true")
    args = parser.parse_args(argv)

    tokens = TokenStore(config.auth_path())
    try:
        require_auth_for_host(args.host, tokens)
    except AuthError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    import uvicorn

    where = "loopback only" if args.host in ("127.0.0.1", "localhost", "::1") else "REACHABLE FROM THE NETWORK"
    print(f"analog on http://{args.host}:{args.port}  ({where})")
    print(f"  auth: {'per-actor tokens' if tokens.enabled else 'off — no tokens configured'}")
    print(f"  data: {config.db_path()}")

    uvicorn.run("server.main:app", host=args.host, port=args.port, reload=args.reload)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
