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


def cors_origins() -> list[str]:
    raw = os.environ.get("ANALOG_CORS_ORIGINS")
    if raw is None:
        return list(DEFAULT_CORS_ORIGINS)
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
