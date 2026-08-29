"""SPEC §2.1 file nodes and §3 POST /media.

Note: openapi.json documents the upload but not the GET that serves the bytes back,
even though contracts/fixtures/canvas.json contains a file node pointing at one.
See AMENDMENTS.md #1.
"""

from __future__ import annotations

import struct
import zlib

import pytest

from tests.conftest import AGENT, HUMAN, make_space

pytestmark = pytest.mark.contract


def tiny_png() -> bytes:
    def chunk(tag, data):
        return (struct.pack(">I", len(data)) + tag + data
                + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))
    return (b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", 1, 1, 8, 0, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(b"\x00\x00", 9))
            + chunk(b"IEND", b""))


@pytest.fixture
def space(client):
    make_space(client, "demo")
    return client


def upload(client, name="shot.png", content_type="image/png", data=None):
    return client.post("/api/spaces/demo/media", params=AGENT,
                       files={"file": (name, data or tiny_png(), content_type)})


def test_upload_returns_a_url_and_metadata(space):
    r = upload(space)
    assert r.status_code == 201, r.text
    body = r.json()
    assert body["content_type"] == "image/png"
    assert body["bytes"] == len(tiny_png())
    assert body["url"].startswith("/api/spaces/demo/media/")
    assert body["url"].endswith(".png")


def test_the_returned_url_serves_the_bytes(space):
    url = upload(space).json()["url"]
    r = space.get(url)
    assert r.status_code == 200
    assert r.content == tiny_png()
    assert r.headers["content-type"].startswith("image/png")


def test_the_url_drops_into_a_file_node(space):
    """SPEC §2.1: binary content is a JSON Canvas file node, so it survives export."""
    url = upload(space).json()["url"]
    r = space.post("/api/spaces/demo/cards", params=AGENT, json={"nodes": [{
        "id": "ignored", "type": "file", "x": 0, "y": 0, "width": 360, "height": 280,
        "file": url, "sp_title": "Current UI"}]})
    assert r.status_code == 201, r.text
    node = r.json()[0]
    assert node["type"] == "file"
    assert node["file"] == url
    assert "sp_kind" not in node, "sp_kind is meaningful only on text nodes"
    assert space.get(node["file"]).status_code == 200


def test_media_is_scoped_to_its_space(client):
    make_space(client, "demo")
    make_space(client, "other")
    url = upload(client).json()["url"]
    assert client.get(url.replace("/demo/", "/other/")).status_code == 404


def test_unknown_media_is_404(space):
    assert space.get("/api/spaces/demo/media/m_nope.png").status_code == 404


def test_uploading_to_an_unknown_space_is_404(client):
    r = client.post("/api/spaces/nope/media", params=AGENT,
                    files={"file": ("a.png", tiny_png(), "image/png")})
    assert r.status_code == 404


def test_upload_emits_no_event(space):
    """There is no media.* event type; the card that references it is the event."""
    before = space.get("/api/spaces/demo/events").json()["events"]
    upload(space)
    assert space.get("/api/spaces/demo/events").json()["events"] == before


@pytest.mark.parametrize("content_type,suffix", [
    ("image/png", ".png"), ("image/jpeg", ".jpg"), ("image/gif", ".gif"),
    ("image/webp", ".webp"), ("image/svg+xml", ".svg"), ("application/pdf", ".pdf"),
])
def test_supported_types(space, content_type, suffix):
    r = upload(space, name=f"f{suffix}", content_type=content_type, data=b"data")
    assert r.status_code == 201, r.text
    assert r.json()["url"].endswith(suffix)


def test_an_unsupported_type_is_rejected(space):
    r = upload(space, name="x.exe", content_type="application/x-msdownload", data=b"MZ")
    assert r.status_code == 400
    assert r.json()["error"] == "unsupported_kind"


def test_a_traversal_filename_cannot_escape_the_media_directory(space):
    """The stored name is server-assigned; the client's filename is advisory only."""
    url = upload(space, name="../../../etc/passwd.png").json()["url"]
    assert ".." not in url
    assert space.get(url).status_code == 200
