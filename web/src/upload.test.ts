import { describe, expect, it } from "vitest";
import {
  acceptFile,
  contentTypeOf,
  FILE_HEIGHT,
  FILE_WIDTH,
  fitCardSize,
  MAX_FILE_H,
  MAX_FILE_W,
  MAX_UPLOAD_BYTES,
  MIN_FILE_H,
  MIN_FILE_W,
  titleOf,
} from "./upload";

describe("contentTypeOf", () => {
  it("trusts a declared type the server accepts", () => {
    expect(contentTypeOf({ name: "x.bin", type: "image/png" })).toBe("image/png");
  });

  it("sniffs from the extension when the type is missing or generic", () => {
    expect(contentTypeOf({ name: "shot.JPEG", type: "" })).toBe("image/jpeg");
    expect(contentTypeOf({ name: "doc.pdf", type: "application/octet-stream" })).toBe("application/pdf");
  });

  it("rejects types the server will not store", () => {
    expect(contentTypeOf({ name: "x.exe", type: "application/x-msdownload" })).toBeNull();
    expect(contentTypeOf({ name: "notes.txt", type: "" })).toBeNull();
  });
});

describe("acceptFile", () => {
  it("accepts a png under the cap", () => {
    expect(acceptFile({ name: "a.png", type: "image/png", size: 1024 }))
      .toEqual({ ok: true, contentType: "image/png" });
  });

  it("rejects an oversized upload before the server does", () => {
    const got = acceptFile({ name: "huge.png", type: "image/png", size: MAX_UPLOAD_BYTES + 1 });
    expect(got.ok).toBe(false);
    if (!got.ok) expect(got.reason).toMatch(/25 MB/);
  });

  it("rejects an unknown type with a reason that names the file", () => {
    const got = acceptFile({ name: "payload.exe", type: "", size: 12 });
    expect(got.ok).toBe(false);
    if (!got.ok) expect(got.reason).toContain("payload.exe");
  });
});

describe("fitCardSize", () => {
  it("falls back to the CLI default when the raster is unknown", () => {
    expect(fitCardSize(0, 0)).toEqual({ width: FILE_WIDTH, height: FILE_HEIGHT });
  });

  it("caps a 4K screenshot inside the sketch box without stretching", () => {
    const { width, height } = fitCardSize(3840, 2160);
    expect(width).toBeLessThanOrEqual(MAX_FILE_W);
    expect(height).toBeLessThanOrEqual(MAX_FILE_H);
    expect(width / height).toBeCloseTo(3840 / 2160, 2);
  });

  it("does not upscale a screenshot that already fits", () => {
    expect(fitCardSize(400, 300)).toEqual({ width: 400, height: 300 });
  });

  it("scales a tiny icon up so the card is still grabable", () => {
    const { width, height } = fitCardSize(64, 64);
    expect(width).toBeGreaterThanOrEqual(MIN_FILE_W);
    expect(height).toBeGreaterThanOrEqual(MIN_FILE_H);
    expect(width).toBe(height);
  });
});

describe("titleOf", () => {
  it("uses the filename, and a generic title for nameless clipboard blobs", () => {
    expect(titleOf({ name: "shot.png" })).toBe("shot.png");
    expect(titleOf({ name: "" })).toBe("image");
    expect(titleOf({ name: "blob" })).toBe("image");
  });
});
