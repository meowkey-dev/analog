#!/usr/bin/env python3
"""Render app/src-tauri/icons/source.png — the Analog app icon.

Drawn in code rather than checked in as a binary so the design is reviewable and
regenerable. No imaging dependency: it rasterises at 4x and box-downsamples, which
is all the antialiasing a few rounded rectangles need.

    python scripts/make_icon.py
    (cd app && npx tauri icon src-tauri/icons/source.png)
"""

from __future__ import annotations

import struct
import sys
import zlib
from pathlib import Path

SIZE = 1024
SS = 4                      # supersampling factor
W = SIZE * SS

# The palette the UI already uses (web/src/styles.css).
BG_TOP = (0x1B, 0x1F, 0x2A)
BG_BOTTOM = (0x10, 0x12, 0x18)
CARD = (0x23, 0x28, 0x33)
CARD_EDGE = (0x39, 0x40, 0x52)
ACCENT = (0x6E, 0xA8, 0xFE)
GREEN = (0x63, 0xC1, 0x87)
PIN = (0xE0, 0xA3, 0x5A)


def rounded_rect(x0, y0, x1, y1, radius):
    """A hit-test for a rounded rectangle, in supersampled coordinates."""
    def inside(px, py):
        if px < x0 or px > x1 or py < y0 or py > y1:
            return False
        cx = min(max(px, x0 + radius), x1 - radius)
        cy = min(max(py, y0 + radius), y1 - radius)
        dx, dy = px - cx, py - cy
        return dx * dx + dy * dy <= radius * radius
    return inside


def thick_line(x0, y0, x1, y1, half):
    """Hit-test for a capsule between two points."""
    vx, vy = x1 - x0, y1 - y0
    length2 = vx * vx + vy * vy or 1.0

    def inside(px, py):
        t = max(0.0, min(1.0, ((px - x0) * vx + (py - y0) * vy) / length2))
        dx = px - (x0 + t * vx)
        dy = py - (y0 + t * vy)
        return dx * dx + dy * dy <= half * half
    return inside


def circle(cx, cy, r):
    def inside(px, py):
        dx, dy = px - cx, py - cy
        return dx * dx + dy * dy <= r * r
    return inside


def render() -> bytearray:
    """One RGB byte triple per supersampled pixel."""
    u = W / 100.0                      # 1 unit = 1% of the icon
    buf = bytearray(W * W * 3)

    outer = rounded_rect(0, 0, W - 1, W - 1, 22 * u)

    # Three cards and the edges between them — the canvas, in miniature.
    cards = [
        (rounded_rect(16 * u, 20 * u, 45 * u, 43 * u, 3 * u), 3.5 * u),
        (rounded_rect(55 * u, 20 * u, 84 * u, 43 * u, 3 * u), 3.5 * u),
        (rounded_rect(30 * u, 57 * u, 70 * u, 82 * u, 3 * u), 4.5 * u),
    ]
    edges = [
        thick_line(45 * u, 31 * u, 55 * u, 31 * u, 1.1 * u),
        thick_line(38 * u, 43 * u, 44 * u, 57 * u, 1.1 * u),
        thick_line(66 * u, 43 * u, 58 * u, 57 * u, 1.1 * u),
    ]
    # The title bars, and the annotation pin that is the point of the whole thing.
    bars = [
        rounded_rect(20 * u, 24 * u, 38 * u, 26.5 * u, 1.2 * u),
        rounded_rect(59 * u, 24 * u, 77 * u, 26.5 * u, 1.2 * u),
        rounded_rect(35 * u, 62 * u, 58 * u, 64.5 * u, 1.2 * u),
    ]
    green_bars = [
        rounded_rect(35 * u, 69 * u, 65 * u, 71.5 * u, 1.2 * u),
        rounded_rect(35 * u, 74 * u, 52 * u, 76.5 * u, 1.2 * u),
    ]
    pin = circle(68 * u, 59 * u, 4.2 * u)
    pin_ring = circle(68 * u, 59 * u, 6.0 * u)

    for y in range(W):
        row = y * W * 3
        fy = y + 0.5
        # Vertical gradient for the plate.
        t = y / (W - 1)
        base = tuple(round(a + (b - a) * t) for a, b in zip(BG_TOP, BG_BOTTOM))
        for x in range(W):
            fx = x + 0.5
            if not outer(fx, fy):
                continue                      # transparent corners handled below
            colour = base
            for hit in edges:
                if hit(fx, fy):
                    colour = CARD_EDGE
                    break
            for hit, _ in cards:
                if hit(fx, fy):
                    colour = CARD
                    break
            for hit in bars:
                if hit(fx, fy):
                    colour = ACCENT
                    break
            for hit in green_bars:
                if hit(fx, fy):
                    colour = GREEN
                    break
            if pin_ring(fx, fy):
                colour = CARD_EDGE
            if pin(fx, fy):
                colour = PIN
            i = row + x * 3
            buf[i] = colour[0]
            buf[i + 1] = colour[1]
            buf[i + 2] = colour[2]
    return buf


def alpha_mask() -> bytearray:
    """1 byte per supersampled pixel: inside the rounded plate or not."""
    u = W / 100.0
    outer = rounded_rect(0, 0, W - 1, W - 1, 22 * u)
    mask = bytearray(W * W)
    for y in range(W):
        fy = y + 0.5
        row = y * W
        for x in range(W):
            if outer(x + 0.5, fy):
                mask[row + x] = 255
    return mask


def downsample(rgb: bytearray, mask: bytearray) -> bytes:
    """Box filter SSxSS down to SIZE, producing straight RGBA."""
    out = bytearray()
    for y in range(SIZE):
        out.append(0)                          # PNG filter byte: none
        for x in range(SIZE):
            r = g = b = a = 0
            for dy in range(SS):
                base = ((y * SS + dy) * W + x * SS)
                for dx in range(SS):
                    i = (base + dx) * 3
                    m = mask[base + dx]
                    r += rgb[i] * m
                    g += rgb[i + 1] * m
                    b += rgb[i + 2] * m
                    a += m
            n = SS * SS
            if a:
                out += bytes((r // a, g // a, b // a, a // n))
            else:
                out += b"\x00\x00\x00\x00"
    return bytes(out)


def png(raw: bytes) -> bytes:
    def chunk(tag: bytes, data: bytes) -> bytes:
        return (struct.pack(">I", len(data)) + tag + data
                + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))

    return (b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", SIZE, SIZE, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9))
            + chunk(b"IEND", b""))


def main() -> int:
    target = Path(sys.argv[1]) if len(sys.argv) > 1 else (
        Path(__file__).resolve().parent.parent / "app" / "src-tauri" / "icons" / "source.png")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(png(downsample(render(), alpha_mask())))
    print(f"wrote {target} ({SIZE}x{SIZE}, {target.stat().st_size} bytes)")
    print("next:  (cd app && npx tauri icon src-tauri/icons/source.png)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
