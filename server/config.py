"""Runtime specifics the contract leaves open.

contracts/openapi.json pins the base URL to http://127.0.0.1:8787/api, so the port
and the /api prefix are contract, not choice. Everything else here is a decision
recorded in DECISIONS.md.
"""

from __future__ import annotations

import os
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent

# --- network -----------------------------------------------------------------
# 8787 comes from openapi.json `servers[0].url`. Bind loopback only: v1 has no auth.
HOST = os.environ.get("ANALOG_HOST", "127.0.0.1")
PORT = int(os.environ.get("ANALOG_PORT", "8787"))
API_PREFIX = "/api"

# Vite dev server. The web app is same-origin in production (server mounts web/dist),
# so CORS only matters during development.
DEFAULT_CORS_ORIGINS = (
    "http://localhost:5173",
    "http://127.0.0.1:5173",
)


# The Tauri shell loads the bundled UI from its own scheme, so a remote server has
# to allow it explicitly. macOS/iOS use tauri://localhost; the others use
# http://tauri.localhost.
TAURI_ORIGINS = ("tauri://localhost", "http://tauri.localhost")


def cors_origins() -> list[str]:
    """`*` is honoured but never the default: an open canvas should be a choice."""
    raw = os.environ.get("ANALOG_CORS_ORIGINS")
    if raw is None:
        return [*DEFAULT_CORS_ORIGINS, *TAURI_ORIGINS]
    return [o.strip() for o in raw.split(",") if o.strip()]


# --- storage -----------------------------------------------------------------
def data_dir() -> Path:
    return Path(os.environ.get("ANALOG_DATA_DIR", REPO_ROOT / "data")).resolve()


def db_path() -> Path:
    override = os.environ.get("ANALOG_DB")
    return Path(override).resolve() if override else data_dir() / "analog.db"


def media_dir() -> Path:
    """Uploads live at <data>/media/<space_id>/<m_ulid>.<ext>.

    Keyed by space id rather than slug so a space rename cannot orphan its media.
    """
    return data_dir() / "media"


def auth_path() -> Path:
    """Per-actor bearer tokens. Absent or empty means auth is off (loopback dev)."""
    override = os.environ.get("ANALOG_AUTH_FILE")
    return Path(override).resolve() if override else data_dir() / "auth.json"


SCHEMA_PATH = Path(__file__).resolve().parent / "schema.sql"

# Largest accepted upload. Arbitrary; a screenshot is ~1MB.
MAX_UPLOAD_BYTES = 25 * 1024 * 1024

MEDIA_EXTENSIONS = {
    "image/png": ".png",
    "image/jpeg": ".jpg",
    "image/gif": ".gif",
    "image/webp": ".webp",
    "image/svg+xml": ".svg",
    "application/pdf": ".pdf",
}
