import { forwardRef } from "react";

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
}>(({ className, srcDoc, title }, ref) => (
  <iframe
    ref={ref}
    className={className}
    sandbox={HTML_CARD_SANDBOX}
    srcDoc={srcDoc}
    title={title}
  />
));
HTMLCardFrame.displayName = "HTMLCardFrame";
