import { forwardRef, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { getConnection, resolveUrl } from "./api";

const VENDOR_PATH = "/vendor/plotly-basic-2.35.2.min.js";

export function serverVendorSource(source: string): string {
  if (!getConnection().baseUrl || !source.includes(VENDOR_PATH)) return source;
  const doc = new DOMParser().parseFromString(source, "text/html");
  let changed = false;
  doc.querySelectorAll("script[src]").forEach((script) => {
    if (script.getAttribute("src") !== VENDOR_PATH) return;
    script.setAttribute("src", resolveUrl(VENDOR_PATH));
    changed = true;
  });
  return changed ? "<!doctype html>\n" + doc.documentElement.outerHTML : source;
}

/**
 * The sandbox is the boundary around agent-authored documents. Scripts may
 * choose their own network endpoint, but they never regain Analog's origin or
 * form submission capability that could be confused with Analog's parent page.
 */
export const HTML_CARD_SANDBOX = "allow-scripts" as const;

/** How long a replaced frame takes to fade out over its successor (#127). */
export const FRAME_FADE_MS = 160;
/** A frame that never acknowledges the scroll restore is shown anyway. */
const RESTORE_TIMEOUT_MS = 400;

type Box = { left: number; top: number; width: number; height: number };

/**
 * One iframe of a card. `live` is the frame in the layout, the one the parent's
 * ref points at; `incoming` loads the next srcDoc invisibly on top of it;
 * `retiring` is the previous live frame fading out over its successor.
 */
export interface FrameSlot {
  key: number;
  srcDoc: string;
  role: "live" | "incoming" | "retiring";
  /** Where an overlaid (incoming or retiring) frame sits: over the live one. */
  box?: Box;
}

/** A new srcDoc loads beside the live frame; only the newest such load is kept. */
export function offerSource(frames: FrameSlot[], srcDoc: string, key: number, box: Box | undefined): FrameSlot[] {
  const live = frames.find((f) => f.role === "live");
  const settled = frames.filter((f) => f.role !== "incoming");
  if (live?.srcDoc === srcDoc) return settled.length === frames.length ? frames : settled;
  if (frames.some((f) => f.role === "incoming" && f.srcDoc === srcDoc)) return frames;
  if (!live || !box) return [...frames.filter((f) => f.role === "retiring"), { key, srcDoc, role: "live" }];
  return [...settled, { key, srcDoc, role: "incoming", box }];
}

/** The loaded frame takes the layout; the one it replaces fades out over it. */
export function promoteFrame(frames: FrameSlot[], key: number, box: Box | undefined): FrameSlot[] {
  if (!frames.some((f) => f.key === key && f.role === "incoming")) return frames;
  return frames.map((f): FrameSlot => {
    if (f.key === key) return { key, srcDoc: f.srcDoc, role: "live" };
    if (f.role === "live") return { ...f, role: "retiring", box };
    return f;
  }).filter((f) => f.role !== "retiring" || f.box);
}

const reducedMotion = () =>
  typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

function boxOf(el: HTMLIFrameElement | undefined): Box | undefined {
  return el ? { left: el.offsetLeft, top: el.offsetTop, width: el.offsetWidth, height: el.offsetHeight } : undefined;
}

/**
 * An html card's frame. Setting srcDoc on a live iframe blanks it while the new
 * document loads, so an agent's every edit flashed the card white and dropped
 * the reader back at the top (#127). Instead the new document loads into a
 * second, invisible frame; once it has loaded and taken the old scroll offset
 * (through the injected reporter's `analog-scroll-to`, since the sandbox keeps
 * the parent out), it takes the old frame's place and the old one fades out
 * over it, then is removed. The forwarded ref always names the live frame.
 */
export const HTMLCardFrame = forwardRef<HTMLIFrameElement, {
  className?: string;
  srcDoc: string;
  title: string;
  onLoad?: () => void;
}>(({ className, srcDoc, title, onLoad }, ref) => {
  const [frames, setFrames] = useState<FrameSlot[]>(() => [{ key: 0, srcDoc, role: "live" }]);
  const nextKey = useRef(1);
  const elements = useRef(new Map<number, HTMLIFrameElement>());
  // The live frame's last reported scroll offset: what its successor restores.
  const scroll = useRef<{ sx: number; sy: number } | null>(null);
  const timers = useRef(new Set<number>());
  const promoted = useRef(new Set<number>());
  const onLoadRef = useRef(onLoad);
  onLoadRef.current = onLoad;
  // Message and timer callbacks fire between renders; this is what they read.
  const framesRef = useRef(frames);
  framesRef.current = frames;
  const live = frames.find((f) => f.role === "live");
  const liveKey = live?.key;

  const later = useCallback((ms: number, run: () => void) => {
    const id = window.setTimeout(() => { timers.current.delete(id); run(); }, ms);
    timers.current.add(id);
  }, []);
  useEffect(() => () => { timers.current.forEach((id) => window.clearTimeout(id)); }, []);

  useEffect(() => {
    setFrames((current) => {
      const liveEl = elements.current.get(current.find((f) => f.role === "live")?.key ?? -1);
      const next = offerSource(current, srcDoc, nextKey.current, boxOf(liveEl));
      if (next.some((f) => f.key === nextKey.current)) nextKey.current += 1;
      return next;
    });
  }, [srcDoc]);

  useLayoutEffect(() => {
    const el = liveKey === undefined ? null : elements.current.get(liveKey) ?? null;
    if (typeof ref === "function") ref(el);
    else if (ref) ref.current = el;
  }, [liveKey, ref]);
  useEffect(() => () => {
    if (typeof ref === "function") ref(null);
    else if (ref) ref.current = null;
  }, [ref]);

  const promote = useCallback((key: number) => {
    // The acknowledgement and the timeout both land here; the first one wins.
    const slot = framesRef.current.find((f) => f.key === key);
    if (slot?.role !== "incoming" || promoted.current.has(key)) return;
    promoted.current.add(key);
    const retired = framesRef.current.find((f) => f.role === "live")?.key;
    const box = boxOf(elements.current.get(retired ?? -1));
    setFrames((current) => promoteFrame(current, key, box));
    elements.current.get(key)?.contentWindow?.postMessage({ type: "analog-report" }, "*");
    later(reducedMotion() ? 0 : FRAME_FADE_MS, () =>
      setFrames((current) => current.filter((f) => f.key !== retired)));
    onLoadRef.current?.();
  }, [later]);

  // Only the live frame's scroll is remembered, and only an incoming frame's
  // acknowledgement of the restore promotes it; event.source tells them apart.
  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const d = event.data as { type?: string; sx?: number; sy?: number; ack?: number } | null;
      if (d?.type !== "analog-scroll") return;
      for (const f of frames) {
        if (elements.current.get(f.key)?.contentWindow !== event.source) continue;
        if (f.role === "live") scroll.current = { sx: Number(d.sx) || 0, sy: Number(d.sy) || 0 };
        if (f.role === "incoming" && d.ack === f.key) {
          scroll.current = { sx: Number(d.sx) || 0, sy: Number(d.sy) || 0 };
          promote(f.key);
        }
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [frames, promote]);

  const loaded = (slot: FrameSlot) => {
    if (slot.role === "live") { onLoadRef.current?.(); return; }
    if (slot.role !== "incoming") return;
    const at = scroll.current;
    if (!at || (at.sx === 0 && at.sy === 0)) { promote(slot.key); return; }
    elements.current.get(slot.key)?.contentWindow?.postMessage(
      { type: "analog-scroll-to", sx: at.sx, sy: at.sy, ack: slot.key }, "*");
    later(RESTORE_TIMEOUT_MS, () => promote(slot.key));
  };

  return (
    <>
      {frames.map((slot) => (
        <iframe
          key={slot.key}
          ref={(el) => {
            if (el) elements.current.set(slot.key, el);
            else elements.current.delete(slot.key);
          }}
          className={slot.role === "live" ? className : `${className ?? ""} html-frame-${slot.role}`}
          style={slot.box ? { position: "absolute", ...slot.box } : undefined}
          sandbox={HTML_CARD_SANDBOX}
          srcDoc={serverVendorSource(slot.srcDoc)}
          title={title}
          aria-hidden={slot.role === "live" ? undefined : true}
          tabIndex={slot.role === "live" ? undefined : -1}
          onLoad={() => loaded(slot)}
        />
      ))}
    </>
  );
});
HTMLCardFrame.displayName = "HTMLCardFrame";
