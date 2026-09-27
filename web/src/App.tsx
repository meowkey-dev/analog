import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Canvas } from "./Canvas";
import { Activity } from "./Activity";
import { AnnotationComposer, AnnotationPanel, type DraftAnnotation } from "./Annotations";
import { Commander } from "./Commander";
import { SpaceIndex, SpaceSwitcher } from "./Spaces";
import { api, subscribe, ApiError, getIdentity } from "./api";
import type { AnalogEvent, Annotation, Canvas as CanvasData, Motivation, Node, Space } from "./api";
import { emptyDrawing, DRAW_WIDTH, DRAW_HEIGHT } from "./draw";
import { ExportMenu } from "./ExportMenu";
import { acceptFile, cardSizeForFile, FILE_CASCADE, isFileDrag, MEDIA_ACCEPT, titleOf } from "./upload";
import { Connect, adopt, attempt, type Connected } from "./Connect";
import { clearConnection, describe, loadConnection } from "./connection";
import { HTMLCardFrame } from "./html-card";
import { Reader } from "./Reader";
import { Changes } from "./Changes";
import { endOfBoard, type ReadingOrder } from "./reading-order";
import {
  baseline, changesSince, lastChange, loadRecord, markAllSeen, markSeen, saveRecord, storageKey,
  type CardChange, type SeenRecord,
} from "./seen";
import { COMPACT, NARROW, useMediaQuery } from "./media";
import fixtureCanvas from "../../contracts/fixtures/canvas.json";
import fixtureAnnotations from "../../contracts/fixtures/annotations.json";
import fixtureEvents from "../../contracts/fixtures/events.json";
import fixtureSpace from "../../contracts/fixtures/space.json";

const EMPTY: CanvasData = { nodes: [], edges: [] };
const NO_CHANGES = new Map<string, CardChange>();

function startsNarrow(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia(NARROW).matches;
}

/** The card a URL points at: /s/slug#c_… */
function hashCard(): string {
  const id = decodeURIComponent(window.location.hash.slice(1));
  return /^c_[A-Za-z0-9_-]+$/.test(id) ? id : "";
}

function setHashCard(id: string | null): void {
  const base = window.location.pathname + window.location.search;
  window.history.replaceState(window.history.state, "", id ? `${base}#${id}` : base);
}

function slugFromPath(path: string): string {
  return path.match(/^\/s\/([a-z0-9-]{1,64})/)?.[1] ?? "";
}

/** Two routes — "/" and "/s/:slug" — is not worth a router dependency. */
function useRoute(): [string, (slug: string) => void] {
  const [path, setPath] = useState(window.location.pathname);

  useEffect(() => {
    const onPop = () => setPath(window.location.pathname);
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  const go = useCallback((slug: string) => {
    const next = slug ? `/s/${slug}` : "/";
    if (next === window.location.pathname) return;
    window.history.pushState(null, "", next + window.location.search);
    setPath(next);
  }, []);

  return [slugFromPath(path), go];
}

type Boot =
  | { phase: "checking" }
  | { phase: "connect"; problem: string | null }
  | { phase: "ready"; connected: Connected };

export default function App() {
  const [slug, go] = useRoute();
  // WP3/WP4 render the fixture space with no database behind it: /s/redesign?fixture
  const fixtureMode = new URLSearchParams(window.location.search).has("fixture");

  const [boot, setBoot] = useState<Boot>({ phase: "checking" });
  const [space, setSpace] = useState<Space | null>(null);
  const [canvas, setCanvas] = useState<CanvasData>(EMPTY);
  const [annotations, setAnnotations] = useState<Annotation[]>([]);
  const [events, setEvents] = useState<AnalogEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [connection, setConnection] = useState<"live" | "polling">("polling");

  const [annotateMode, setAnnotateMode] = useState(false);
  const [draft, setDraft] = useState<DraftAnnotation | null>(null);
  const [selectedCard, setSelectedCard] = useState<string | null>(null);
  const [selectedEdge, setSelectedEdge] = useState<string | null>(null);
  const [selectedAnnotation, setSelectedAnnotation] = useState<string | null>(null);
  const [focus, setFocus] = useState<{ id: string; nonce: number } | null>(null);
  const [showResolved, setShowResolved] = useState(false);
  const narrow = useMediaQuery(NARROW);
  // The full toolbar needs a wide window; below this it folds into ⋯.
  const compact = useMediaQuery(COMPACT);
  // A phone opens on the reader; a desktop on the canvas. Either can switch.
  const [view, setView] = useState<"canvas" | "reader">(() => (startsNarrow() ? "reader" : "canvas"));
  const [readerId, setReaderId] = useState<string | null>(null);
  const [readerOrder, setReaderOrder] = useState<ReadingOrder>("board");
  const [editRequest, setEditRequest] = useState(0);
  const [menuOpen, setMenuOpen] = useState(false);
  const [quickOpen, setQuickOpen] = useState(false);
  const quickPhoto = useRef<HTMLInputElement>(null);
  const [loadedSlug, setLoadedSlug] = useState<string | null>(null);
  const [rightPanel, setRightPanel] = useState<"comments" | "activity" | "changes" | null>(
    () => (startsNarrow() ? null : "comments"));
  const [popOut, setPopOut] = useState<Node | null>(null);
  const [commander, setCommander] = useState(false);
  const [pendingEdit, setPendingEdit] = useState<string | null>(null);
  const picker = useRef<HTMLInputElement>(null);
  // Markdown text scale, remembered per browser (#14).
  const [mdScale, setMdScale] = useState(() => {
    const stored = Number(localStorage.getItem("analog.mdscale"));
    return Number.isFinite(stored) && stored > 0 ? stored : 1;
  });

  useEffect(() => {
    document.documentElement.style.setProperty("--md-scale", String(mdScale));
    localStorage.setItem("analog.mdscale", String(mdScale));
  }, [mdScale]);

  const notify = useCallback((message: string) => {
    setToast(message);
    window.setTimeout(() => setToast(null), 4000);
  }, []);

  const failed = useCallback((exc: unknown) => {
    if (exc instanceof ApiError && (exc.status === 401 || exc.status === 403)) {
      setBoot({ phase: "connect", problem: exc.status === 401
        ? "The server stopped accepting that token."
        : `${exc.message}` });
      return;
    }
    const message = exc instanceof ApiError
      ? (exc.code === "conflict"
        ? "Someone else changed that card first — reloading."
        : `${exc.code}: ${exc.message}`)
      : String(exc);
    notify(message);
  }, [notify]);

  // --- loading ---------------------------------------------------------------

  const refresh = useCallback(async () => {
    if (fixtureMode) return;
    try {
      const [nextCanvas, nextAnnotations] = await Promise.all([
        api.getCanvas(slug, true),
        api.listAnnotations(slug),
      ]);
      setCanvas(nextCanvas);
      setAnnotations(nextAnnotations);
    } catch (exc) {
      failed(exc);
    }
  }, [slug, fixtureMode, failed]);

  // Settle on a server and an identity before touching any space: which actor we
  // write as is decided by the token, not by this UI (contract 0.3.0).
  useEffect(() => {
    if (fixtureMode) {
      setBoot({ phase: "ready", connected: null as unknown as Connected });
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const connected = await attempt(loadConnection());
        if (cancelled) return;
        adopt(connected);
        setBoot({ phase: "ready", connected });
      } catch (exc) {
        if (!cancelled) {
          setBoot({ phase: "connect", problem: exc instanceof Error ? exc.message : String(exc) });
        }
      }
    })();
    return () => { cancelled = true; };
  }, [fixtureMode]);

  useEffect(() => {
    if (fixtureMode) {
      setSpace(fixtureSpace as Space);
      setCanvas(fixtureCanvas as CanvasData);
      setAnnotations(fixtureAnnotations as Annotation[]);
      setEvents((fixtureEvents as { events: AnalogEvent[] }).events);
      setLoadedSlug(slug);
      return;
    }
    if (!slug || boot.phase !== "ready") return;   // "/" renders the space index
    setError(null);
    (async () => {
      try {
        const [nextSpace, nextCanvas, nextAnnotations, log] = await Promise.all([
          api.getSpace(slug),
          api.getCanvas(slug, true),
          api.listAnnotations(slug),
          api.listEvents(slug),
        ]);
        setSpace(nextSpace);
        setCanvas(nextCanvas);
        setAnnotations(nextAnnotations);
        setEvents(log.events);
        setLoadedSlug(slug);
      } catch (exc) {
        setError(exc instanceof ApiError ? `${exc.code}: ${exc.message}` : String(exc));
      }
    })();
  }, [slug, fixtureMode, boot.phase]);

  // --- live (SSE, falling back to polling) -----------------------------------

  const seenSeq = useRef(0);
  useEffect(() => {
    if (fixtureMode || !slug || !space) return;
    seenSeq.current = space.seq;
    const stop = subscribe(slug, space.seq, (event) => {
      if (event.seq <= seenSeq.current) return;
      seenSeq.current = event.seq;
      if (event.type === "space.deleted") {
        // The space is gone; refreshing it would only 404.
        notify("This space was deleted.");
        go("");
        return;
      }
      setEvents((previous) => (previous.some((e) => e.seq === event.seq)
        ? previous
        : [...previous, event]));
      void refresh();
    }, setConnection);
    return stop;
    // Re-subscribing on every seq change would thrash; space identity is enough.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slug, fixtureMode, space?.id, refresh, go, notify]);

  // --- mutations -------------------------------------------------------------

  const guard = <T,>(work: () => Promise<T>) => {
    if (fixtureMode) {
      notify("Fixture mode is read-only.");
      return;
    }
    work().catch(failed);
  };

  const liveNodes = useMemo(
    () => canvas.nodes.filter((node) => !node.sp_deleted_at),
    [canvas.nodes],
  );

  // --- what changed since you last looked (seen.ts) ------------------------------

  const latestSeq = useMemo(
    () => events.reduce((max, e) => Math.max(max, e.seq), space?.seq ?? 0),
    [events, space?.seq],
  );
  const latestSeqRef = useRef(latestSeq);
  latestSeqRef.current = latestSeq;
  const seenKey = storageKey(describe(loadConnection()), slug);
  const seenKeyRef = useRef(seenKey);
  seenKeyRef.current = seenKey;
  const [seen, setSeen] = useState<SeenRecord | null>(null);

  // Read the card a link points at before anything can rewrite the hash: the
  // reader reports its first card as soon as the board loads.
  const pendingHash = useRef("");
  useEffect(() => {
    const id = hashCard();
    pendingHash.current = id;
    setSeen(null);
    setReaderId(id || null);
    setReaderOrder("board");
  }, [slug]);

  // The first visit to a space takes the board as already seen: only what
  // happens after it is news.
  useEffect(() => {
    if (!slug || loadedSlug !== slug) return;
    const stored = loadRecord(seenKey);
    if (stored) {
      setSeen(stored);
      return;
    }
    const first = baseline(liveNodes, latestSeqRef.current);
    saveRecord(seenKey, first);
    setSeen(first);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadedSlug, seenKey]);

  const me = fixtureMode ? "human" : getIdentity().actor;
  const changes = useMemo(() => {
    if (!seen) return NO_CHANGES;
    const current = liveNodes.filter((n) => !n.sp_superseded_by);
    return changesSince(seen, current, events, me);
  }, [seen, liveNodes, events, me]);
  const changesRef = useRef(changes);
  changesRef.current = changes;
  const lastChanges = useMemo(() => {
    const map = new Map<string, string>();
    if (!seen) return map;
    for (const node of liveNodes) {
      const before = lastChange(seen, node);
      if (before !== undefined) map.set(node.id, before);
    }
    return map;
  }, [seen, liveNodes]);

  const updateSeen = useCallback((change: (record: SeenRecord) => SeenRecord) => {
    setSeen((previous) => {
      if (!previous) return previous;
      const next = change(previous);
      saveRecord(seenKeyRef.current, next);
      return next;
    });
  }, []);

  const markSeenNodes = useCallback((nodes: Node[]) => {
    if (nodes.length > 0) updateSeen((r) => markSeen(r, nodes, latestSeqRef.current));
  }, [updateSeen]);

  const markSeenId = useCallback((id: string) => {
    const node = canvasRef.current.nodes.find((n) => n.id === id);
    if (node) markSeenNodes([node]);
  }, [markSeenNodes]);

  const markAllSeenNow = useCallback(() => {
    const nodes = canvasRef.current.nodes.filter((n) => !n.sp_deleted_at);
    updateSeen((r) => markAllSeen(r, nodes, latestSeqRef.current));
  }, [updateSeen]);

  // --- reader navigation ---------------------------------------------------------

  const readerIdRef = useRef<string | null>(null);
  readerIdRef.current = readerId;

  // Leaving a card in the reader is what acknowledges it: the diff stays up for
  // as long as the card does.
  const navigateReader = useCallback((id: string) => {
    const previous = readerIdRef.current;
    if (previous && previous !== id && changesRef.current.has(previous)) markSeenId(previous);
    readerIdRef.current = id;
    setReaderId(id);
    setHashCard(id);
  }, [markSeenId]);

  const switchView = useCallback((next: "canvas" | "reader") => {
    if (next === "canvas") {
      const current = readerIdRef.current;
      if (current && changesRef.current.has(current)) markSeenId(current);
      setHashCard(null);
      if (current) {
        setSelectedCard(current);
        setFocus({ id: current, nonce: Date.now() });
      }
    } else {
      setAnnotateMode(false);
      setDraft(null);
      if (selectedCard) navigateReader(selectedCard);
    }
    setView(next);
  }, [markSeenId, navigateReader, selectedCard]);

  /** Go to a card in whichever view is showing. */
  const showCard = useCallback((id: string) => {
    if (view === "reader") {
      navigateReader(id);
    } else {
      setFocus({ id, nonce: Date.now() });
      setSelectedCard(id);
    }
    if (narrow) setRightPanel(null);
  }, [view, narrow, navigateReader]);

  // A link to a card (/s/slug#c_…) opens on it, once the board is loaded.
  const hashHandled = useRef<string | null>(null);
  useEffect(() => {
    if (!slug || loadedSlug !== slug || !seen || hashHandled.current === slug) return;
    hashHandled.current = slug;
    const id = pendingHash.current;
    if (id && liveNodes.some((n) => n.id === id)) {
      showCard(id);
      return;
    }
    // Nothing asked for: a phone with news opens on the news.
    if (view === "reader" && changes.size > 0) setReaderOrder("changes");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadedSlug, seen, slug]);

  // --- undo (#2) ---------------------------------------------------------------
  // Inverse actions only: moves, resizes, edits, creations, links, resolves.
  // A delete has no inverse — the frozen API cannot restore a soft-deleted card.

  type UndoStep = { label: string; run: () => void };
  const undoStack = useRef<UndoStep[]>([]);
  const [undoDepth, setUndoDepth] = useState(0);
  const canvasRef = useRef(canvas);
  canvasRef.current = canvas;

  const pushUndo = useCallback((step: UndoStep) => {
    undoStack.current = [...undoStack.current.slice(-49), step];
    setUndoDepth(undoStack.current.length);
  }, []);

  // Undo never crosses spaces: inverse actions close over this space's canvas.
  useEffect(() => {
    undoStack.current = [];
    setUndoDepth(0);
  }, [slug]);

  const undo = useCallback(() => {
    const step = undoStack.current[undoStack.current.length - 1];
    if (!step) return;
    undoStack.current = undoStack.current.slice(0, -1);
    setUndoDepth(undoStack.current.length);
    step.run();
    notify(`Undid: ${step.label}`);
  }, [notify]);

  const patchNode = (id: string, patch: Partial<Node>) =>
    setCanvas((c) => ({ ...c, nodes: c.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)) }));

  const moveCard = (id: string, x: number, y: number) => {
    const prev = canvasRef.current.nodes.find((n) => n.id === id);
    if (prev && (prev.x !== x || prev.y !== y)) {
      const back = { x: prev.x, y: prev.y };
      pushUndo({
        label: "move",
        run: () => {
          patchNode(id, back);
          guard(() => api.updateCard(slug, id, back));
        },
      });
    }
    patchNode(id, { x, y });
    guard(() => api.updateCard(slug, id, { x, y }));
  };

  // A west/north resize moves the card as well (#37), so the patch — and the
  // undo that mirrors it — carries whatever keys actually changed.
  const resizeCard = (id: string, rect: { x?: number; y?: number; width?: number; height?: number }) => {
    const prev = canvasRef.current.nodes.find((n) => n.id === id);
    const back: typeof rect = {};
    for (const key of ["x", "y", "width", "height"] as const) {
      if (rect[key] !== undefined && prev && prev[key] !== rect[key]) back[key] = prev[key];
    }
    if (Object.keys(back).length > 0) {
      pushUndo({
        label: "resize",
        run: () => {
          patchNode(id, back);
          guard(() => api.updateCard(slug, id, back));
        },
      });
    }
    patchNode(id, rect);
    guard(() => api.updateCard(slug, id, rect));
  };

  const editCard = (id: string, text: string, title?: string) => {
    const current = canvasRef.current.nodes.find((n) => n.id === id);
    const commit = async (next: string, nextTitle?: string) => {
      // Re-read the rev at call time: an undo may run long after the edit.
      const now = canvasRef.current.nodes.find((n) => n.id === id);
      const patch: Partial<Node> = { text: next };
      if (nextTitle !== undefined) patch.sp_title = nextTitle;
      const node = await api.updateCard(slug, id, patch, now?.sp_rev);
      setCanvas((c) => ({ ...c, nodes: c.nodes.map((n) => (n.id === id ? node : n)) }));
      // Your own edit is not news to you.
      markSeenNodes([node]);
      // Branch mode answers with the new revision; the reader follows it.
      if (node.id !== id && readerIdRef.current === id) navigateReader(node.id);
    };
    const titleChanged = title !== undefined && current && (current.sp_title ?? "") !== title;
    if (current && ((current.text ?? "") !== text || titleChanged)) {
      const previous = current.text ?? "";
      const previousTitle = titleChanged ? current.sp_title ?? "" : undefined;
      pushUndo({ label: "edit", run: () => guard(() => commit(previous, previousTitle)) });
    }
    guard(() => commit(text, titleChanged ? title : undefined));
  };

  const deleteCard = (id: string) => {
    guard(async () => {
      await api.deleteCard(slug, id);
      await refresh();
    });
    setSelectedCard(null);
  };

  const createCardAt = (x: number, y: number, kind: "md" | "draw" = "md", onCreated?: (id: string) => void) => {
    guard(async () => {
      const draft = kind === "draw"
        ? { title: "Sketch", content: emptyDrawing(), kind: "svg" as const,
            x, y, width: DRAW_WIDTH, height: DRAW_HEIGHT }
        : { title: "Untitled", content: "", kind: "md" as const, x, y };
      const [node] = await api.createCards(slug, [draft]);
      markSeenNodes([node!]);
      await refresh();
      setSelectedCard(node!.id);
      if (kind === "draw") setPendingEdit(node!.id);
      onCreated?.(node!.id);
      pushUndo({
        label: kind === "draw" ? "new sketch" : "new card",
        run: () => {
          setSelectedCard(null);
          guard(async () => {
            await api.deleteCard(slug, node!.id);
            await refresh();
          });
        },
      });
    });
  };

  // post /media then a file node. `at` is the drop/paste point; omitted for the
  // picker so the server auto-lays the card out the way `analog upload` does.
  const createFileCards = (files: File[], at?: { x: number; y: number }, onCreated?: (id: string) => void) => {
    const accepted: { file: File; contentType: string }[] = [];
    for (const file of files) {
      const verdict = acceptFile(file);
      if (verdict.ok) accepted.push({ file, contentType: verdict.contentType });
      else notify(verdict.reason);
    }
    if (accepted.length === 0) return;
    guard(async () => {
      const drafts: Array<Partial<Node>> = [];
      for (const [i, item] of accepted.entries()) {
        const media = await api.uploadMedia(slug, item.file, item.contentType);
        const size = await cardSizeForFile(item.file);
        const node: Partial<Node> = {
          type: "file",
          file: media.url,
          sp_title: titleOf(item.file),
          width: size.width,
          height: size.height,
        };
        if (at) {
          node.x = at.x + i * FILE_CASCADE;
          node.y = at.y + i * FILE_CASCADE;
        }
        drafts.push(node);
      }
      const nodes = await api.createNodes(slug, drafts);
      markSeenNodes(nodes);
      await refresh();
      const last = nodes[nodes.length - 1];
      if (last) {
        setSelectedCard(last.id);
        onCreated?.(last.id);
      }
      const ids = nodes.map((n) => n.id);
      pushUndo({
        label: ids.length === 1 ? "upload" : `upload ${ids.length}`,
        run: () => {
          setSelectedCard(null);
          guard(async () => {
            for (const id of ids) await api.deleteCard(slug, id);
            await refresh();
          });
        },
      });
    });
  };

  const createLink = (from: string, to: string, label: string, color: string | null) => {
    guard(async () => {
      const edge = await api.createLink(slug, from, to, label, undefined, color ?? undefined);
      await refresh();
      pushUndo({
        label: "link",
        run: () => {
          setSelectedEdge(null);
          guard(async () => {
            await api.deleteLink(slug, edge.id);
            await refresh();
          });
        },
      });
    });
  };

  const deleteLink = (id: string) => {
    const edge = canvasRef.current.edges.find((e) => e.id === id);
    guard(async () => {
      await api.deleteLink(slug, id);
      await refresh();
    });
    setSelectedEdge(null);
    // The id is gone, so the inverse recreates the edge — same ends, label, sides, color.
    if (edge) {
      pushUndo({
        label: "delete link",
        run: () => guard(async () => {
          await api.createLink(slug, edge.fromNode, edge.toNode, edge.label,
            { fromSide: edge.fromSide, toSide: edge.toSide }, edge.color);
          await refresh();
        }),
      });
    }
  };

  const submitAnnotation = (body: string, motivation: Motivation) => {
    if (!draft) return;
    guard(async () => {
      await api.createAnnotation(slug, draft.cardId, body, draft.selector, motivation);
      await refresh();
    });
    setDraft(null);
  };

  const resolveAnnotation = (id: string, reply: string) => {
    pushUndo({
      label: "resolve",
      run: () => {
        guard(async () => {
          await api.resolveAnnotation(slug, id, undefined, false);
          await refresh();
        });
      },
    });
    guard(async () => {
      await api.resolveAnnotation(slug, id, reply || undefined);
      await refresh();
    });
  };

  const reopenAnnotation = (id: string) => {
    guard(async () => {
      await api.resolveAnnotation(slug, id, undefined, false);
      await refresh();
    });
  };

  // --- keyboard --------------------------------------------------------------

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      // The commander opens from anywhere, including mid-edit (#12).
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setCommander((open) => !open);
        return;
      }
      const tag = (event.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      if ((event.target as HTMLElement)?.closest?.(".draw-editor")) return;
      if ((event.metaKey || event.ctrlKey) && !event.shiftKey && event.key.toLowerCase() === "z") {
        // Not intercepted inside inputs, so text editing keeps its native undo.
        event.preventDefault();
        undo();
        return;
      }
      if (event.key === "Escape") {
        setDraft(null);
        setAnnotateMode(false);
        setPopOut(null);
        setCommander(false);
        setSelectedCard(null);
        setSelectedEdge(null);
        setMenuOpen(false);
        setQuickOpen(false);
      }
      if (event.key === "r" && !event.metaKey && !event.ctrlKey && !event.altKey) {
        switchView(view === "reader" ? "canvas" : "reader");
        return;
      }
      // The reader has no selection to delete and no pins to drop.
      if (view === "reader") return;
      if (event.key === "c" && !event.metaKey && !event.ctrlKey) {
        setAnnotateMode((on) => !on);
      }
      if (event.key === "Backspace" || event.key === "Delete") {
        if (selectedEdge) deleteLink(selectedEdge);
        else if (selectedCard) deleteCard(selectedCard);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  // a drop onto the topbar (or anywhere that is not the canvas) must not navigate
  // the tab to the file. the canvas handles placement; everywhere else is a no-op.
  useEffect(() => {
    const allow = (event: DragEvent) => {
      if (event.dataTransfer && isFileDrag(event.dataTransfer.types)) event.preventDefault();
    };
    window.addEventListener("dragover", allow);
    window.addEventListener("drop", allow);
    return () => {
      window.removeEventListener("dragover", allow);
      window.removeEventListener("drop", allow);
    };
  }, []);

  if (boot.phase === "checking") {
    return <div className="index"><header><h1>analog</h1><p>connecting…</p></header></div>;
  }

  if (boot.phase === "connect") {
    return (
      <Connect
        initial={loadConnection()}
        problem={boot.problem}
        onConnected={(connected) => setBoot({ phase: "ready", connected })}
      />
    );
  }

  if (!slug && !fixtureMode) {
    return <SpaceIndex onOpen={go} release={boot.connected.health.release} />;
  }

  if (error) {
    const missing = error.startsWith("not_found");
    return (
      <div className="fatal">
        <h1>Analog</h1>
        <p>{missing ? `There is no space called "${slug}".` : error}</p>
        <p className="hint">
          {missing ? (
            <><a href="/" onClick={(event) => { event.preventDefault(); go(""); }}>
              See all spaces
            </a>, or create this one with <code>analog new {slug}</code>.</>
          ) : (
            <>Start the server with <code>analog-server</code>, or open{" "}
            <a href="/s/redesign?fixture">/s/redesign?fixture</a> to render the fixture space
            with no database.</>
          )}
        </p>
      </div>
    );
  }

  const draftCard = draft ? canvas.nodes.find((n) => n.id === draft.cardId) : null;
  const openCount = annotations.filter((a) => !a.resolved).length;
  const togglePanel = (panel: "comments" | "activity" | "changes") =>
    setRightPanel(rightPanel === panel ? null : panel);
  const boardEnd = () => endOfBoard(liveNodes.filter((n) => !n.sp_superseded_by));

  // A quick-added card opens straight into the reader's editor: on a phone the
  // canvas is too small to type into.
  const quickAddText = () => {
    setQuickOpen(false);
    const at = boardEnd();
    createCardAt(at.x, at.y, "md", (id) => {
      setView("reader");
      navigateReader(id);
      setEditRequest((n) => n + 1);
    });
  };

  const secondary = (
    <>
      <button data-closes onClick={() => setCommander(true)} title="Locate a card (⌘K)">⌘K find</button>
      <button data-closes onClick={undo} disabled={undoDepth === 0} title="Undo the last action (⌘Z)">
        ⎌ undo{undoDepth > 0 ? ` (${undoDepth})` : ""}
      </button>
      <div className="text-scale" title="Text size for markdown cards">
        <button onClick={() => setMdScale((s) => Math.max(0.75, Math.round((s - 0.125) * 1000) / 1000))}
                disabled={mdScale <= 0.75}>A−</button>
        <span>{Math.round(mdScale * 100)}%</span>
        <button onClick={() => setMdScale((s) => Math.min(2, Math.round((s + 0.125) * 1000) / 1000))}
                disabled={mdScale >= 2}>A+</button>
      </div>
      <button data-closes onClick={() => picker.current?.click()} title="Upload an image onto the board">
        upload
      </button>
      <ExportMenu
        title={space?.title ?? slug}
        slug={slug}
        onError={notify}
        onBusy={(message) => setToast(message)}
      />
      {view === "canvas" && (
        <button data-closes className={annotateMode ? "on" : ""} onClick={() => setAnnotateMode((on) => !on)}
                title="Click a card to pin a comment; shift-drag for a region (c)">
          {annotateMode ? "commenting…" : "comment"}
        </button>
      )}
      <button data-closes onClick={() => togglePanel("comments")}
              className={rightPanel === "comments" ? "on" : ""}>
        comments{openCount > 0 ? ` (${openCount})` : ""}
      </button>
      <button data-closes onClick={() => togglePanel("activity")}
              className={rightPanel === "activity" ? "on" : ""}>
        activity
      </button>
      <button
        className="server"
        title={boot.phase === "ready" && boot.connected
          ? `${describe(loadConnection())} · writing as ${getIdentity().actor}`
          : "connection"}
        onClick={() => {
          clearConnection();
          setBoot({ phase: "connect", problem: null });
        }}
      >
        {describe(loadConnection())}
        <span className="who">{getIdentity().actor}</span>
      </button>
    </>
  );

  const panelClose = narrow ? () => setRightPanel(null) : undefined;

  return (
    <div className={`app view-${view}${narrow ? " narrow" : ""}`}>
      <header className="topbar">
        {!narrow && (
          <a className="brand" href="/"
             onClick={(event) => { event.preventDefault(); go(""); }}>analog</a>
        )}
        {fixtureMode
          ? <div className="space-name">{space?.title ?? slug}<span className="slug">/{slug}</span>
              <span className="badge">fixture</span></div>
          : <SpaceSwitcher current={slug} title={space?.title ?? slug} onOpen={go} />}
        <div className="spacer" />
        <button className="view-toggle" onClick={() => switchView(view === "reader" ? "canvas" : "reader")}
                title={view === "reader" ? "Show the canvas (r)" : "Read one card at a time (r)"}>
          {view === "reader" ? "◱ canvas" : "▤ reader"}
        </button>
        <button onClick={() => togglePanel("changes")}
                className={`changes-button${rightPanel === "changes" ? " on" : ""}${changes.size > 0 ? " has" : ""}`}
                title="What changed since you last looked">
          {narrow ? (changes.size > 0 ? `● ${changes.size}` : "✓") : `changes${changes.size > 0 ? ` (${changes.size})` : ""}`}
        </button>
        {!compact && secondary}
        <span className={`conn ${connection}`} title={connection === "live" ? "live over SSE" : "polling"}>
          {connection === "live" ? "live" : "poll"}
        </span>
        {compact && (
          <div className="overflow-wrap">
            <button className={menuOpen ? "on" : ""} onClick={() => setMenuOpen((o) => !o)}
                    aria-label="More" aria-expanded={menuOpen}>⋯</button>
            {menuOpen && (
              <>
                <div className="menu-backdrop" onClick={() => setMenuOpen(false)} />
                <div className="overflow-menu" role="menu"
                     onClick={(event) => {
                       if ((event.target as Element).closest("[data-closes]")) setMenuOpen(false);
                     }}>
                  {narrow && (
                    <a className="brand" href="/"
                       onClick={(event) => { event.preventDefault(); setMenuOpen(false); go(""); }}>
                      all spaces
                    </a>
                  )}
                  {secondary}
                </div>
              </>
            )}
          </div>
        )}
        <input
          ref={picker}
          type="file"
          hidden
          multiple
          accept={MEDIA_ACCEPT}
          onChange={(event) => {
            const files = Array.from(event.target.files ?? []);
            event.target.value = "";
            if (files.length > 0) createFileCards(files);
          }}
        />
        <input
          ref={quickPhoto}
          type="file"
          hidden
          accept="image/*"
          onChange={(event) => {
            const files = Array.from(event.target.files ?? []);
            event.target.value = "";
            if (files.length === 0) return;
            createFileCards(files, boardEnd(), (id) => showCard(id));
          }}
        />
      </header>

      <div className="body">
        {view === "canvas" ? (
          <Canvas
            nodes={liveNodes}
            allNodes={canvas.nodes}
            edges={canvas.edges}
            annotations={annotations}
            annotateMode={annotateMode}
            draft={draft}
            selectedCard={selectedCard}
            selectedEdge={selectedEdge}
            selectedAnnotation={selectedAnnotation}
            focus={focus}
            onSelectCard={setSelectedCard}
            onSelectEdge={setSelectedEdge}
            onSelectAnnotation={setSelectedAnnotation}
            onDraft={setDraft}
            onMoveCard={moveCard}
            onResizeCard={resizeCard}
            onEditCard={editCard}
            onDeleteCard={deleteCard}
            onCreateLink={createLink}
            onDeleteLink={deleteLink}
            onPopOut={setPopOut}
            onCreateCardAt={createCardAt}
            onCreateFileCards={(files, x, y) => createFileCards(files, { x, y })}
            pendingEdit={pendingEdit}
            onConsumedPendingEdit={() => setPendingEdit(null)}
            changes={changes}
            lastChanges={lastChanges}
            onMarkSeen={markSeenId}
          />
        ) : (
          <Reader
            nodes={liveNodes}
            edges={canvas.edges}
            annotations={annotations}
            events={events}
            changes={changes}
            lastChanges={lastChanges}
            currentId={readerId}
            order={readerOrder}
            editRequest={editRequest}
            onOrder={setReaderOrder}
            onNavigate={navigateReader}
            onMarkSeen={markSeenId}
            onMarkAllSeen={markAllSeenNow}
            onEdit={editCard}
            onDraft={setDraft}
            onResolve={resolveAnnotation}
            onReopen={reopenAnnotation}
            onShowOnCanvas={(id) => {
              readerIdRef.current = id;
              switchView("canvas");
            }}
            onFind={() => setCommander(true)}
            onPopOut={setPopOut}
            onQuickAdd={() => setQuickOpen(true)}
            notify={notify}
          />
        )}

        {narrow && rightPanel && <div className="sheet-backdrop" onClick={() => setRightPanel(null)} />}
        {rightPanel === "comments" && (
          <AnnotationPanel
            annotations={annotations}
            showResolved={showResolved}
            selectedId={selectedAnnotation}
            onToggleResolved={() => setShowResolved((s) => !s)}
            onSelect={(id) => {
              setSelectedAnnotation(id);
              const annotation = annotations.find((a) => a.id === id);
              if (annotation) showCard(annotation.card_id);
            }}
            onResolve={resolveAnnotation}
            onReopen={reopenAnnotation}
            onClose={panelClose}
          />
        )}
        {rightPanel === "activity" && (
          <Activity events={events} nodes={canvas.nodes} onFocus={showCard} onClose={panelClose} />
        )}
        {rightPanel === "changes" && (
          <Changes
            changes={changes}
            nodes={liveNodes}
            onFocus={showCard}
            onMarkSeen={markSeenId}
            onMarkAllSeen={markAllSeenNow}
            onReview={() => {
              setRightPanel(narrow ? null : rightPanel);
              setReaderOrder("changes");
              if (view !== "reader") switchView("reader");
            }}
            onClose={panelClose}
          />
        )}
      </div>

      {narrow && view === "canvas" && !draft && (
        <button className="fab" onClick={() => setQuickOpen(true)} aria-label="Add a card">＋</button>
      )}

      {quickOpen && (
        <div className="action-sheet" onClick={() => setQuickOpen(false)}>
          <div className="action-sheet-inner" role="menu" onClick={(e) => e.stopPropagation()}>
            <button onClick={quickAddText}>Text card</button>
            <button onClick={() => { setQuickOpen(false); quickPhoto.current?.click(); }}>
              Photo or image
            </button>
            <button className="ghost" onClick={() => setQuickOpen(false)}>Cancel</button>
          </div>
        </div>
      )}

      {draft && draftCard && (
        <AnnotationComposer
          draft={draft}
          cardTitle={draftCard.sp_title || draftCard.id}
          onCancel={() => setDraft(null)}
          onDismissQuote={() => setDraft((d) => (d ? { ...d, quote: undefined } : d))}
          onSubmit={submitAnnotation}
        />
      )}

      {popOut && (
        <div className="popout" onClick={() => setPopOut(null)}>
          <div className="popout-inner" onClick={(e) => e.stopPropagation()}>
            <header>
              {popOut.sp_title || popOut.id}
              <button onClick={() => setPopOut(null)}>close</button>
            </header>
            <HTMLCardFrame srcDoc={popOut.text ?? ""} title={popOut.sp_title ?? popOut.id} />
          </div>
        </div>
      )}

      {commander && (
        <Commander
          nodes={liveNodes}
          onClose={() => setCommander(false)}
          onFocusCard={showCard}
        />
      )}

      {toast && <div className="toast">{toast}</div>}
      {!narrow && (
        <footer className="hints">
          {view === "reader" ? (
            <>← → or j/k for the previous or next card · ⌘K to jump to a card · <strong>r for the
            canvas</strong> · Δ / diff shows what changed since you last looked</>
          ) : annotateMode ? (
            <>click a card to pin a comment · <strong>shift-drag on a card to comment on a
            region</strong> · esc to stop</>
          ) : (
            <>drag to pan · ⌘/ctrl-scroll to zoom · scroll over a card to scroll the card ·
            double-click empty space for a card · <strong>shift-double-click for a
            sketch</strong> · drop or paste an image onto the board · drag the ◇ handle to link ·
            double-click a card to edit · ✎ on an svg card to draw · ⌘K to find a card ·
            ⌘Z to undo · c to comment · r to read card by card</>
          )}
        </footer>
      )}
    </div>
  );
}
