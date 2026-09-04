import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import Markdown from "react-markdown";
import DOMPurify from "dompurify";
import { DiffView } from "./Diff";
import { DrawEditor } from "./DrawEditor";
import { AnnotationOverlay, type DraftAnnotation } from "./Annotations";
import { api, getConnection, resolveUrl } from "./api";
import type { Annotation, Node } from "./api";
import { HTMLCardFrame } from "./html-card";
import { mdRemarkPlugins, mdRehypePlugins } from "./markdown";
import "katex/dist/katex.min.css";

/**
 * A file node's URL cannot go straight into <img src>: an image request carries no
 * Authorization header, so on a server with tokens it would 401. Fetch it and hand
 * back an object URL instead. With no token the plain URL is fine and cheaper.
 */
function useMediaSrc(path: string | undefined): string | undefined {
  const [src, setSrc] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (!path) {
      setSrc(undefined);
      return;
    }
    if (!getConnection().token) {
      setSrc(resolveUrl(path));
      return;
    }
    let objectUrl: string | null = null;
    let cancelled = false;
    api.mediaObjectUrl(path)
      .then((url) => {
        if (cancelled) { URL.revokeObjectURL(url); return; }
        objectUrl = url;
        setSrc(url);
      })
      .catch(() => { if (!cancelled) setSrc(undefined); });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [path]);

  return src;
}

/**
 * Markdown card themes (#41). The frozen contract has nowhere to store per-card
 * UI state, so the choice is a per-browser preference in localStorage, keyed by
 * card id; `default` is the absence of a choice and inherits the card chrome.
 */
export const MD_THEMES = [
  { id: "default", label: "Default", swatch: "#1d212a" },
  { id: "nord", label: "Nord", swatch: "#2e3440" },
  { id: "paper", label: "Paper", swatch: "#f7f5f0" },
  { id: "solar", label: "Solarized", swatch: "#fdf6e3" },
] as const;

export type MdTheme = (typeof MD_THEMES)[number]["id"];

const mdThemeKey = (id: string) => `analog.mdtheme.${id}`;

function loadMdTheme(id: string): MdTheme {
  const stored = localStorage.getItem(mdThemeKey(id));
  return MD_THEMES.some((t) => t.id === stored) ? (stored as MdTheme) : "default";
}

/** Where a card can be grabbed to resize (#37). Edges and corners, not just se. */
export type ResizeDir = "n" | "s" | "e" | "w" | "ne" | "nw" | "se" | "sw";

const RESIZE_DIRS: ResizeDir[] = ["n", "s", "e", "w", "ne", "nw", "se", "sw"];

/**
 * Renders one card body by sp_kind (SPEC §5).
 *
 *   plain -> <pre>            md -> react-markdown + KaTeX (#68)
 *   svg   -> inlined, sanitized; double-click / ✎ opens a pen (#61), not a textarea
 *   html  -> <iframe sandbox="allow-scripts"> with NO allow-same-origin, so agent
 *            HTML can neither read the parent document nor forge an annotation.
 *            The renderer prepends one script of its own (SCROLL_REPORTER) that
 *            posts scroll/size metrics — and on request a text quote — out to the
 *            parent, so pins can follow the content (#23). It grants the frame no
 *            capability and exfiltrates nothing; a card that spoofs those messages
 *            only displaces its own pins.
 *   file  -> <img>, because binary content is a JSON Canvas file node (§2.1).
 */
function Body({ node, mdTheme, bodyRef }: {
  node: Node; mdTheme: MdTheme; bodyRef: (el: HTMLElement | null) => void;
}) {
  const kind = node.type === "file" ? "file" : (node.sp_kind ?? "plain");

  const svg = useMemo(
    () => (kind === "svg" ? DOMPurify.sanitize(node.text ?? "", { USE_PROFILES: { svg: true, svgFilters: true }, ADD_ATTR: ["data-analog-stroke"] }) : ""),
    [kind, node.text],
  );
  // Injecting at render time, never into the stored text: the card stays verbatim
  // on the wire and in the editor.
  const html = useMemo(
    () => (kind === "html" ? withScrollReporter(node.text ?? "") : ""),
    [kind, node.text],
  );

  switch (kind) {
    case "md":
      return (
        <div ref={bodyRef} className={`card-body md md-theme-${mdTheme}`}>
          <Markdown remarkPlugins={mdRemarkPlugins} rehypePlugins={mdRehypePlugins}>{node.text ?? ""}</Markdown>
        </div>
      );
    case "svg":
      return <div ref={bodyRef} className="card-body svg" dangerouslySetInnerHTML={{ __html: svg }} />;
    case "html":
      return (
        <HTMLCardFrame
          ref={bodyRef}
          className="card-body html"
          srcDoc={html}
          title={node.sp_title ?? node.id}
        />
      );
    case "file":
      return <FileBody node={node} bodyRef={bodyRef} />;
    default:
      return <pre ref={bodyRef} className="card-body plain">{node.text ?? ""}</pre>;
  }
}

/**
 * Prepended into html card content at render time (issue #23). Reports the
 * document's scroll/size metrics so the parent can anchor pins to the content
 * rather than the visible box, and quotes the text under a pin on request.
 * Kept dependency-free and read-only: it observes the frame's own document and
 * talks to the parent that put it there.
 */
const SCROLL_REPORTER = [
  "<script>(function(){",
  "var raf=0;",
  "function metrics(){",
  "var de=document.documentElement,b=document.body;",
  "return{cw:Math.max(de.scrollWidth,b?b.scrollWidth:0),ch:Math.max(de.scrollHeight,b?b.scrollHeight:0),",
  "vw:de.clientWidth,vh:de.clientHeight,sx:de.scrollLeft||(b?b.scrollLeft:0),sy:de.scrollTop||(b?b.scrollTop:0)};}",
  "function send(){var m=metrics();m.type='analog-scroll';parent.postMessage(m,'*');}",
  "function soon(){cancelAnimationFrame(raf);raf=requestAnimationFrame(send);}",
  "function quoteFor(vx,vy,vw,vh){",
  "var t={left:vx,top:vy,right:vx+vw,bottom:vy+vh},parts=[],w=document.createTreeWalker(document,NodeFilter.SHOW_TEXT),n,range=null,rects,i,out='';",
  "while((n=w.nextNode())){",
  "if(!n.nodeValue||!n.nodeValue.trim())continue;",
  "range=range||document.createRange();range.selectNodeContents(n);",
  "rects=range.getClientRects();",
  "for(i=0;i<rects.length;i++){",
  "if(rects[i].left<=t.right&&rects[i].right>=t.left&&rects[i].top<=t.bottom&&rects[i].bottom>=t.top){parts.push(n.nodeValue);break;}}}",
  "out=parts.join(' ').replace(/\\s+/g,' ').trim();",
  "return out.length>240?out.slice(0,240)+'…':out;}",
  "addEventListener('scroll',soon,true);",
  "addEventListener('resize',soon);",
  "addEventListener('load',send);",
  "if(self.ResizeObserver)new ResizeObserver(soon).observe(document.documentElement);",
  "addEventListener('message',function(e){",
  "if(e.source!==parent)return;",
  "var d=e.data;",
  "if(!d||d.type!=='analog-quote')return;",
  "var t=quoteFor(d.vx||0,d.vy||0,d.vw||0,d.vh||0);",
  "if(t)parent.postMessage({type:'analog-quote',text:t},'*');});",
  "send();",
  "})();</script>",
].join("");

/** Insert after the doctype if there is one; ahead of it would flip quirks mode. */
function withScrollReporter(html: string): string {
  const doctype = /^\s*<!DOCTYPE[^>]*>/i.exec(html);
  return doctype
    ? html.replace(doctype[0], doctype[0] + SCROLL_REPORTER)
    : SCROLL_REPORTER + html;
}

function FileBody({ node, bodyRef }: { node: Node; bodyRef: (el: HTMLElement | null) => void }) {
  const src = useMediaSrc(node.file);
  const isImage = !/\.pdf$/i.test(node.file ?? "");
  if (!src) return <div ref={bodyRef} className="card-body file muted">loading…</div>;
  return (
    <div ref={bodyRef} className="card-body file">
      {isImage
        ? <img src={src} alt={node.sp_title ?? ""} draggable={false} />
        : <a data-file-card-link="true" href={src} target="_blank" rel="noreferrer">
            {node.sp_title || node.file}
          </a>}
    </div>
  );
}

export interface CardProps {
  node: Node;
  /** The card that replaced this one, when superseded — the diff target (#6). */
  successor?: Node;
  selected: boolean;
  editing: boolean;
  openCount: number;
  revisions: number;
  collapsed: boolean;
  /** Unresolved comments on this card: the in-card thread (#5) and the overlay pins. */
  thread: Annotation[];
  threadOpen: boolean;
  annotateMode: boolean;
  /** The in-flight pin, while annotate mode is drafting on this card. */
  draft: DraftAnnotation | null;
  selectedAnnotation: string | null;
  onToggleThread: (id: string) => void;
  onSelectAnnotation: (id: string) => void;
  onDraft: (draft: DraftAnnotation) => void;
  onToggleCollapsed: (id: string) => void;
  onPointerDownHeader: (event: React.PointerEvent, node: Node) => void;
  onPointerDownResize: (event: React.PointerEvent, node: Node, dir: ResizeDir) => void;
  onPointerDownLink: (event: React.PointerEvent, node: Node) => void;
  onSelect: (id: string) => void;
  onStartEdit: (id: string) => void;
  onCommitEdit: (id: string, text: string) => void;
  onCancelEdit: () => void;
  onDelete: (id: string) => void;
  onPopOut: (node: Node) => void;
}

function CardView(props: CardProps) {
  const { node, selected, editing, collapsed, revisions, openCount } = props;
  const superseded = Boolean(node.sp_superseded_by);
  const kind = node.type === "file" ? "file" : (node.sp_kind ?? "plain");
  // Superseded cards can show what the revision changed (#6).
  const [view, setView] = useState<"content" | "diff">("content");
  const [mdTheme, setMdTheme] = useState<MdTheme>(() => loadMdTheme(node.id));
  const [themeOpen, setThemeOpen] = useState(false);
  // The scrollable body element (the iframe itself for html): the annotation
  // overlay measures it to anchor pins to the content (#23).
  const bodyRef = useRef<HTMLElement | null>(null);
  const setBody = useCallback((el: HTMLElement | null) => { bodyRef.current = el; }, []);

  const chooseMdTheme = (theme: MdTheme) => {
    setMdTheme(theme);
    localStorage.setItem(mdThemeKey(node.id), theme);
    setThemeOpen(false);
  };

  const style: React.CSSProperties = {
    left: node.x,
    top: node.y,
    width: node.width,
    height: superseded && collapsed ? undefined : node.height,
  };

  // A superseded card collapses to a stub so a long chain doesn't swamp the canvas.
  if (superseded && collapsed) {
    return (
      <div className={`card stub${selected ? " selected" : ""}`} style={style}
           data-card-id={node.id} onPointerDown={() => props.onSelect(node.id)}>
        <div className="card-head" onPointerDown={(e) => props.onPointerDownHeader(e, node)}>
          <span className="card-title">{node.sp_title || node.id}</span>
          <span className="badge">rev {revisions}</span>
          {/* Same trailing buttons, same order, as the expanded header: toggling
              must not move the collapse control onto where delete lands (#9). */}
          <button className="icon" title="Show the superseded content"
                  onClick={() => props.onToggleCollapsed(node.id)}>▾</button>
          <button className="icon danger" title="Delete card" onClick={() => props.onDelete(node.id)}>×</button>
        </div>
      </div>
    );
  }

  return (
    <div
      className={`card${selected ? " selected" : ""}${superseded ? " superseded" : ""}`}
      style={style}
      data-card-id={node.id}
      onPointerDown={() => {
        setThemeOpen(false);
        props.onSelect(node.id);
      }}
      onDoubleClick={(e) => {
        if (node.type === "text" && !superseded) {
          e.stopPropagation();
          props.onStartEdit(node.id);
        }
      }}
    >
      <div className="card-head" onPointerDown={(e) => props.onPointerDownHeader(e, node)}>
        <span className="card-title">{node.sp_title || node.id}</span>
        <span className="card-kind">{kind}</span>
        {openCount > 0 && <span className="badge comments" title={`${openCount} open comment(s)`}>{openCount}</span>}
        {props.thread.length > 0 && (
          <button className={`icon${props.threadOpen ? " on" : ""}`}
                  title="Show the comment thread on this card"
                  onClick={() => props.onToggleThread(node.id)}>💬</button>
        )}
        {superseded && (
          <button className="icon" title="Collapse" onClick={() => props.onToggleCollapsed(node.id)}>▴</button>
        )}
        {kind === "html" && (
          <button className="icon" title="Open full window" onClick={() => props.onPopOut(node)}>⤢</button>
        )}
        {kind === "svg" && !superseded && !editing && (
          <button className="icon" title="Draw on this card"
                  onClick={(e) => { e.stopPropagation(); props.onStartEdit(node.id); }}>✎</button>
        )}
        {kind === "md" && !superseded && (
          <div className="theme-wrap"
               onPointerDown={(e) => e.stopPropagation()}
               onKeyDown={(e) => { if (e.key === "Escape") setThemeOpen(false); }}>
            <button className={`icon${themeOpen ? " on" : ""}`} title="Markdown theme"
                    onClick={() => setThemeOpen((o) => !o)}>🎨</button>
            {themeOpen && (
              <div className="theme-menu" role="menu" aria-label="Markdown theme">
                {MD_THEMES.map((t) => (
                  <button key={t.id} role="menuitem" className={t.id === mdTheme ? "on" : ""}
                          onClick={() => chooseMdTheme(t.id)}>
                    <span className="swatch" style={{ background: t.swatch }} />
                    {t.label}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
        <button className="icon danger" title="Delete card" onClick={() => props.onDelete(node.id)}>×</button>
      </div>

      {superseded && (
        <div className="superseded-note">
          <span>superseded — read only</span>
          {props.successor && props.successor.text !== undefined && node.text !== undefined && (
            <span className="diff-toggle">
              <button className={view === "content" ? "on" : ""} onClick={() => setView("content")}
                      title="Show this revision's content">content</button>
              <button className={view === "diff" ? "on" : ""} onClick={() => setView("diff")}
                      title="Show what changed vs the next revision">diff</button>
            </span>
          )}
        </div>
      )}

      {/* The zone the overlay shares with the content: pins are positioned and
          clipped against the scrollable body, not the whole card (#23). */}
      <div className="body-zone">
        {superseded && view === "diff" && props.successor && props.successor.text !== undefined ? (
          <DiffView before={node.text ?? ""} after={props.successor.text ?? ""} />
        ) : editing && kind === "svg" ? (
          <DrawEditor
            text={node.text ?? ""}
            onCommit={(text) => props.onCommitEdit(node.id, text)}
            onCancel={props.onCancelEdit}
          />
        ) : editing ? (
          <textarea
            className="card-body editor"
            defaultValue={node.text ?? ""}
            autoFocus
            onPointerDown={(e) => e.stopPropagation()}
            onBlur={(e) => props.onCommitEdit(node.id, e.currentTarget.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") props.onCancelEdit();
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                props.onCommitEdit(node.id, e.currentTarget.value);
              }
            }}
          />
        ) : (
          <Body node={node} mdTheme={mdTheme} bodyRef={setBody} />
        )}

        <AnnotationOverlay
          node={node}
          bodyRef={bodyRef}
          annotations={props.thread}
          active={props.annotateMode && !editing}
          draft={props.draft}
          selectedId={props.selectedAnnotation}
          onSelect={props.onSelectAnnotation}
          onDraft={props.onDraft}
        />
      </div>

      {props.threadOpen && props.thread.length > 0 && (
        <div className="card-thread" onPointerDown={(e) => e.stopPropagation()}>
          {props.thread.map((a) => (
            <div key={a.id}
                 className={`thread-item${props.selectedAnnotation === a.id ? " selected" : ""}`}
                 onClick={() => props.onSelectAnnotation(a.id)}>
              <span className="who">{a.creator}</span>
              <span className="body">{a.body}</span>
              {a.stale && <span className="badge stale" title="The card changed after this was written">stale</span>}
            </div>
          ))}
        </div>
      )}

      {!superseded && (
        <>
          {RESIZE_DIRS.map((dir) => (
            <div key={dir} className={`handle resize ${dir}`} title="Resize"
                 onPointerDown={(e) => props.onPointerDownResize(e, node, dir)} />
          ))}
          <div className="handle link" title="Drag to another card to link"
               onPointerDown={(e) => props.onPointerDownLink(e, node)} />
        </>
      )}
      <div className="card-foot">
        <span>{node.sp_created_by}</span>
        <span>rev {node.sp_rev ?? 1}</span>
      </div>
    </div>
  );
}

export const Card = memo(CardView);
