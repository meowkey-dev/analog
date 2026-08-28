# Analog

A shared canvas for one human and their agents. See [SPEC.md](SPEC.md).

`contracts/` and `server/schema.sql` are frozen (see `contracts/README.md`).
Runtime choices the contract leaves open are in [DECISIONS.md](DECISIONS.md);
gaps found in the contract are in [AMENDMENTS.md](AMENDMENTS.md).

## Setup

```bash
uv venv --python 3.14
uv pip install -e ".[dev,mcp]"
(cd web && /opt/homebrew/bin/npm install)   # see DECISIONS.md: `npm` on PATH is Bun's shim
```

## Run

```bash
python scripts/seed.py --reset     # load contracts/fixtures/ into a fresh DB
.venv/bin/uvicorn server.main:app --host 127.0.0.1 --port 8787
(cd web && /opt/homebrew/bin/npm run dev)   # http://localhost:5173/s/redesign
```

## Test

```bash
.venv/bin/python -m pytest            # the contract suite
(cd web && /opt/homebrew/bin/npm run build)
```

`tests/contract/` is written against `contracts/`, not against the implementation.
Tests needing a running app skip until `server.main.create_app()` exists.

## Layout

    server/     FastAPI + SQLite, the only place business logic lives
    client/     typed HTTP client over the API
    cli/        `analog`
    mcp_server/ FastMCP stdio server (not `mcp/` — see DECISIONS.md)
    skill/      the agent skill
    web/        React + Vite
    scripts/    seed.py
    tests/      contract suite
