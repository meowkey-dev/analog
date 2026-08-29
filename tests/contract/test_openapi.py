"""The contract document itself is valid and covers SPEC §3."""

from __future__ import annotations

import pytest
from openapi_spec_validator import validate as validate_spec

pytestmark = pytest.mark.contract

# SPEC §3, plus /feedback which contracts/README.md documents as a correction to it.
SPEC_ENDPOINTS = {
    ("/spaces", "post"), ("/spaces", "get"),
    ("/spaces/{slug}", "get"), ("/spaces/{slug}", "patch"), ("/spaces/{slug}", "delete"),
    ("/spaces/{slug}/canvas", "get"),
    ("/spaces/{slug}/import", "post"),
    ("/spaces/{slug}/cards", "post"),
    ("/spaces/{slug}/cards/{card_id}", "patch"),
    ("/spaces/{slug}/cards/{card_id}", "delete"),
    ("/spaces/{slug}/links", "post"),
    ("/spaces/{slug}/links/{link_id}", "delete"),
    ("/spaces/{slug}/annotations", "get"), ("/spaces/{slug}/annotations", "post"),
    ("/spaces/{slug}/annotations/{annotation_id}", "patch"),
    ("/spaces/{slug}/feedback", "get"),
    ("/spaces/{slug}/events", "get"),
    ("/spaces/{slug}/events/stream", "get"),
    ("/spaces/{slug}/media", "post"),
}

MUTATING = [
    (p, m) for (p, m) in SPEC_ENDPOINTS
    if m in ("post", "patch", "delete")
]


def test_spec_is_valid_openapi_31(openapi):
    validate_spec(openapi)
    assert openapi["openapi"] == "3.1.0"


def test_every_spec_endpoint_is_documented(openapi):
    documented = {
        (path, method)
        for path, ops in openapi["paths"].items()
        for method in ops
        if method in ("get", "post", "patch", "put", "delete")
    }
    assert SPEC_ENDPOINTS <= documented


def test_base_url_pins_the_port(openapi):
    """The port is contract, not a runtime choice: server/config.py must match."""
    from analog.server import config

    assert openapi["servers"][0]["url"] == "http://127.0.0.1:8787/api"
    assert config.PORT == 8787
    assert config.API_PREFIX == "/api"


@pytest.mark.parametrize("path,method", sorted(MUTATING))
def test_mutating_operations_require_actor(openapi, path, method):
    """SPEC §3 and §2.2: actor is mandatory everywhere, with no default."""
    op = openapi["paths"][path][method]
    names = {
        openapi["components"]["parameters"][p["$ref"].rsplit("/", 1)[-1]]["name"]
        if "$ref" in p else p["name"]
        for p in op.get("parameters", [])
    }
    assert {"actor", "actor_kind"} <= names, f"{method.upper()} {path} is missing actor params"
    for ref in ("actor", "actorKind"):
        assert openapi["components"]["parameters"][ref]["required"] is True


def test_feedback_requires_actor_but_not_actor_kind(openapi):
    """A cursor is keyed by actor name alone (schema.sql actor_cursor PK)."""
    params = openapi["paths"]["/spaces/{slug}/feedback"]["get"]["parameters"]
    names = {
        openapi["components"]["parameters"][p["$ref"].rsplit("/", 1)[-1]]["name"]
        if "$ref" in p else p["name"]
        for p in params
    }
    assert "actor" in names
    assert "actor_kind" not in names
    assert {"since", "advance"} <= names


def test_no_whole_canvas_replace(openapi):
    """SPEC §10: destructive bulk semantics are deliberately absent."""
    canvas_ops = set(openapi["paths"]["/spaces/{slug}/canvas"]) - {"parameters"}
    assert canvas_ops == {"get"}
    assert "put" not in openapi["paths"]["/spaces/{slug}/import"]
