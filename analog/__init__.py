"""Analog — a shared canvas for one human and their agents.

Four subpackages, and the split is by surface rather than by layer:

    analog.server       FastAPI + SQLite. Every rule lives in store.py.
    analog.client       typed HTTP client over that API
    analog.cli          `analog`
    analog.mcp_server   FastMCP stdio server

They sit under one top-level name because four generic ones — `server`, `client`,
`cli` — are four chances to collide with whatever else is installed alongside.
"""

__version__ = "0.1.0"
