"""The Error schema in contracts/openapi.json, and nothing else.

FastAPI's default 422 body does not match the contract, so RequestValidationError is
remapped to a 400 `validation_failed` in main.create_app().
"""

from __future__ import annotations

from typing import Any


class ApiError(Exception):
    status = 400
    code = "validation_failed"

    def __init__(self, message: str, **detail: Any):
        super().__init__(message)
        self.message = message
        self.detail = detail or None

    # openapi.json's 409 for updateCard puts the current node at the top level, not
    # under `detail`, so that key is promoted.
    PROMOTED = ("current",)

    def body(self) -> dict:
        out = {"error": self.code, "message": self.message}
        detail = dict(self.detail or {})
        for key in self.PROMOTED:
            if key in detail:
                out[key] = detail.pop(key)
        if detail:
            out["detail"] = detail
        return out


class NotFound(ApiError):
    status = 404
    code = "not_found"


class Conflict(ApiError):
    status = 409
    code = "conflict"


class ActorRequired(ApiError):
    status = 400
    code = "actor_required"


class Unauthorized(ApiError):
    status = 401
    code = "unauthorized"


class Forbidden(ApiError):
    status = 403
    code = "forbidden"


class ValidationFailed(ApiError):
    status = 400
    code = "validation_failed"


class UnsupportedKind(ApiError):
    status = 400
    code = "unsupported_kind"
