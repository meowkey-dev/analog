/**
 * file cards on the board (#67). the API already stores binaries as JSON Canvas
 * file nodes; this is only how the human hands one over — drop, paste, or the
 * picker — and how big the card should be.
 */

/** matches internal/config.MaxUploadBytes; a screenshot is ~1MB. */
export const MAX_UPLOAD_BYTES = 25 * 1024 * 1024;

/** default card box when the file has no raster size, matching `analog upload`. */
export const FILE_WIDTH = 360;
export const FILE_HEIGHT = 280;

/** cap so a 4K screenshot does not fill the space; floor so a 64px icon is still grabable. */
export const MAX_FILE_W = 480;
export const MAX_FILE_H = 360;
export const MIN_FILE_W = 200;
export const MIN_FILE_H = 140;

/** stagger for a multi-file drop so the cards are not stacked on one point. */
export const FILE_CASCADE = 32;

/**
 * content-type → stored extension, matching internal/config.MediaExtensions.
 * the server keys off the part's Content-Type, so the client must send one of these
 * rather than application/octet-stream.
 */
export const MEDIA_TYPES: Record<string, string> = {
  "image/png": ".png",
  "image/jpeg": ".jpg",
  "image/gif": ".gif",
  "image/webp": ".webp",
  "image/svg+xml": ".svg",
  "application/pdf": ".pdf",
};

const MEDIA_BY_EXT: Record<string, string> = {
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".svg": "image/svg+xml",
  ".pdf": "application/pdf",
};

export const MEDIA_ACCEPT = Object.keys(MEDIA_TYPES).join(",");

export type AcceptResult =
  | { ok: true; contentType: string }
  | { ok: false; reason: string };

export function contentTypeOf(file: { name: string; type: string }): string | null {
  const declared = file.type.split(";")[0]!.trim().toLowerCase();
  if (declared && declared in MEDIA_TYPES) return declared;
  const dot = file.name.lastIndexOf(".");
  if (dot < 0) return null;
  return MEDIA_BY_EXT[file.name.slice(dot).toLowerCase()] ?? null;
}

export function acceptFile(file: { name: string; type: string; size: number }): AcceptResult {
  if (file.size > MAX_UPLOAD_BYTES) {
    return { ok: false, reason: `${file.name || "file"} is larger than 25 MB` };
  }
  const contentType = contentTypeOf(file);
  if (!contentType) {
    return { ok: false, reason: `${file.name || "that file"} is not an image or PDF the board can hold` };
  }
  return { ok: true, contentType };
}

export function titleOf(file: { name: string }): string {
  const name = file.name.trim();
  return name && name !== "blob" ? name : "image";
}

/** scale a raster into the sketch-sized box, preserving aspect ratio. */
export function fitCardSize(naturalW: number, naturalH: number): { width: number; height: number } {
  if (!(naturalW > 0) || !(naturalH > 0)) {
    return { width: FILE_WIDTH, height: FILE_HEIGHT };
  }
  const scale = Math.min(1, MAX_FILE_W / naturalW, MAX_FILE_H / naturalH);
  let width = Math.max(1, Math.round(naturalW * scale));
  let height = Math.max(1, Math.round(naturalH * scale));
  if (width < MIN_FILE_W || height < MIN_FILE_H) {
    const up = Math.max(MIN_FILE_W / width, MIN_FILE_H / height);
    width = Math.round(width * up);
    height = Math.round(height * up);
  }
  return { width, height };
}

export async function cardSizeForFile(file: File): Promise<{ width: number; height: number }> {
  const type = contentTypeOf(file);
  if (!type || !type.startsWith("image/") || type === "image/svg+xml") {
    return { width: FILE_WIDTH, height: FILE_HEIGHT };
  }
  const raster = await rasterSize(file);
  if (!raster) return { width: FILE_WIDTH, height: FILE_HEIGHT };
  return fitCardSize(raster.width, raster.height);
}

async function rasterSize(file: File): Promise<{ width: number; height: number } | null> {
  try {
    const bitmap = await createImageBitmap(file);
    const size = { width: bitmap.width, height: bitmap.height };
    bitmap.close();
    return size;
  } catch {
    return null;
  }
}

export function filesFromDataTransfer(data: DataTransfer | null): File[] {
  if (!data) return [];
  if (data.files.length > 0) return Array.from(data.files);
  const fromItems: File[] = [];
  for (const item of Array.from(data.items)) {
    if (item.kind !== "file") continue;
    const file = item.getAsFile();
    if (file) fromItems.push(file);
  }
  return fromItems;
}

export function isFileDrag(types: readonly string[] | DOMStringList): boolean {
  return Array.from(types as ArrayLike<string>).includes("Files");
}
