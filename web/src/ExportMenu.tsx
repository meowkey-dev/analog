import { useEffect, useRef, useState } from "react";
import { buildExportHTML, downloadText, fileStem, waitForPrintReady } from "./export";

/**
 * HTML is a file you can mail; PDF is the browser printing that file (#72).
 * With a card selected the same button exports that card, so there is one
 * export control rather than one per card head; the board stays a menu item.
 */
export function ExportMenu({ title, slug, card, onError, onBusy }: {
  title: string;
  slug: string;
  card?: { id: string; title?: string };
  onError: (message: string) => void;
  onBusy: (message: string | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const [working, setWorking] = useState(false);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const away = (event: MouseEvent) => {
      if (!box.current?.contains(event.target as globalThis.Node)) setOpen(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    window.addEventListener("mousedown", away);
    window.addEventListener("keydown", escape);
    return () => {
      window.removeEventListener("mousedown", away);
      window.removeEventListener("keydown", escape);
    };
  }, [open]);

  const run = async (mode: "html" | "pdf", target?: { id: string; title?: string }) => {
    setOpen(false);
    setWorking(true);
    onBusy(mode === "pdf" ? "preparing PDF…" : "saving HTML…");
    // open the print window before the first await so a click still counts as
    // user activation; a later window.open is often blocked.
    const preview = mode === "pdf" ? window.open("about:blank", "_blank") : null;
    if (mode === "pdf" && !preview) {
      setWorking(false);
      onBusy(null);
      onError("allow pop-ups to export a PDF, or save HTML and print it");
      return;
    }
    try {
      const html = target
        ? await buildExportHTML({ title: target.title || target.id, slug, cardId: target.id })
        : await buildExportHTML({ title, slug });
      if (mode === "html") {
        const name = target ? fileStem(target.title, target.id) : slug;
        downloadText(`${name}.html`, html, "text/html;charset=utf-8");
      } else if (preview) {
        preview.document.open();
        preview.document.write(html);
        preview.document.close();
        await waitForPrintReady(preview);
        preview.focus();
        preview.print();
      }
    } catch (exc) {
      preview?.close();
      onError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setWorking(false);
      onBusy(null);
    }
  };

  const what = card ? "card" : "board";
  return (
    <div className="export-wrap" ref={box}>
      <button onClick={() => setOpen((o) => !o)} disabled={working}
              title={card ? "Save the selected card as HTML or PDF" : "Save the board as HTML or PDF"}>
        {working ? "exporting…" : card ? "export card" : "export"}
      </button>
      {open && (
        <div className="export-menu" role="menu">
          <button role="menuitem" onClick={() => void run("html", card)}
                  title={`A portable HTML snapshot of the ${what} with Analog media embedded`}>
            HTML
            <span className="muted">portable HTML snapshot</span>
          </button>
          <button role="menuitem" onClick={() => void run("pdf", card)}
                  title={`Print the ${what}; choose Save as PDF`}>
            PDF
            <span className="muted">print dialog</span>
          </button>
          {card && (
            <>
              <button role="menuitem" onClick={() => void run("html")}
                      title="A portable HTML snapshot of every card on the board">
                board HTML
                <span className="muted">the whole board, not the card</span>
              </button>
              <button role="menuitem" onClick={() => void run("pdf")}
                      title="Print every card on the board; choose Save as PDF">
                board PDF
                <span className="muted">the whole board, not the card</span>
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
