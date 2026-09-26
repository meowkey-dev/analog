import { forwardRef } from "react";
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

export const HTMLCardFrame = forwardRef<HTMLIFrameElement, {
  className?: string;
  srcDoc: string;
  title: string;
  onLoad?: () => void;
}>(({ className, srcDoc, title, onLoad }, ref) => (
  <iframe
    ref={ref}
    className={className}
    sandbox={HTML_CARD_SANDBOX}
    srcDoc={serverVendorSource(srcDoc)}
    title={title}
    onLoad={onLoad}
  />
));
HTMLCardFrame.displayName = "HTMLCardFrame";
