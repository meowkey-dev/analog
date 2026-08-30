import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Canvas } from "./Canvas";
import { Activity } from "./Activity";
import { AnnotationComposer, AnnotationPanel, type DraftAnnotation } from "./Annotations";
import { SpaceIndex, SpaceSwitcher } from "./Spaces";
import { api, subscribe, ApiError, getIdentity } from "./api";
import type { AnalogEvent, Annotation, Canvas as CanvasData, Motivation, Node, Space } from "./api";
import { Connect, adopt, attempt, type Connected } from "./Connect";
import { clearConnection, describe, loadConnection } from "./connection";
import fixtureCanvas from "../../contracts/fixtures/canvas.json";
import fixtureAnnotations from "../../contracts/fixtures/annotations.json";
import fixtureEvents from "../../contracts/fixtures/events.json";
import fixtureSpace from "../../contracts/fixtures/space.json";

const EMPTY: CanvasData = { nodes: [], edges: [] };

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
  const [rightPanel, setRightPanel] = useState<"comments" | "activity" | null>("comments");
  const [popOut, setPopOut] = useState<Node | null>(null);

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

  const moveCard = (id: string, x: number, y: number) => {
    setCanvas((c) => ({ ...c, nodes: c.nodes.map((n) => (n.id === id ? { ...n, x, y } : n)) }));
    guard(() => api.updateCard(slug, id, { x, y }));
  };

  const resizeCard = (id: string, width: number, height: number) => {
    setCanvas((c) => ({
      ...c,
      nodes: c.nodes.map((n) => (n.id === id ? { ...n, width, height } : n)),
    }));
    guard(() => api.updateCard(slug, id, { width, height }));
  };

  const editCard = (id: string, text: string) => {
    const current = canvas.nodes.find((n) => n.id === id);
    guard(async () => {
      const node = await api.updateCard(slug, id, { text }, current?.sp_rev);
      setCanvas((c) => ({ ...c, nodes: c.nodes.map((n) => (n.id === id ? node : n)) }));
    });
  };

  const deleteCard = (id: string) => {
    guard(async () => {
      await api.deleteCard(slug, id);
      await refresh();
    });
    setSelectedCard(null);
  };

  const createCardAt = (x: number, y: number) => {
    guard(async () => {
      const [node] = await api.createCards(slug, [
        { title: "Untitled", content: "", kind: "md", x, y },
      ]);
      await refresh();
      setSelectedCard(node!.id);
    });
  };

  const createLink = (from: string, to: string, label: string) => {
    guard(async () => {
      await api.createLink(slug, from, to, label);
      await refresh();
    });
  };

  const deleteLink = (id: string) => {
    guard(async () => {
      await api.deleteLink(slug, id);
      await refresh();
    });
    setSelectedEdge(null);
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
      const tag = (event.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      if (event.key === "Escape") {
        setDraft(null);
        setAnnotateMode(false);
        setPopOut(null);
        setSelectedCard(null);
        setSelectedEdge(null);
      }
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
    return <SpaceIndex onOpen={go} />;
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

  return (
    <div className="app">
      <header className="topbar">
        <a className="brand" href="/"
           onClick={(event) => { event.preventDefault(); go(""); }}>analog</a>
        {fixtureMode
          ? <div className="space-name">{space?.title ?? slug}<span className="slug">/{slug}</span>
              <span className="badge">fixture</span></div>
          : <SpaceSwitcher current={slug} title={space?.title ?? slug} onOpen={go} />}
        <div className="spacer" />
        <button className={annotateMode ? "on" : ""} onClick={() => setAnnotateMode((on) => !on)}
                title="Click a card to pin a comment; shift-drag for a region (c)">
          {annotateMode ? "commenting…" : "comment"}
        </button>
        <button onClick={() => setRightPanel(rightPanel === "comments" ? null : "comments")}
                className={rightPanel === "comments" ? "on" : ""}>
          comments{annotations.filter((a) => !a.resolved).length > 0
            ? ` (${annotations.filter((a) => !a.resolved).length})` : ""}
        </button>
        <button onClick={() => setRightPanel(rightPanel === "activity" ? null : "activity")}
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
        <span className={`conn ${connection}`} title={connection === "live" ? "live over SSE" : "polling"}>
          {connection === "live" ? "live" : "poll"}
        </span>
      </header>

      <div className="body">
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
        />

        {rightPanel === "comments" && (
          <AnnotationPanel
            annotations={annotations}
            showResolved={showResolved}
            selectedId={selectedAnnotation}
            onToggleResolved={() => setShowResolved((s) => !s)}
            onSelect={(id) => {
              setSelectedAnnotation(id);
              const annotation = annotations.find((a) => a.id === id);
              if (annotation) setFocus({ id: annotation.card_id, nonce: Date.now() });
            }}
            onResolve={resolveAnnotation}
            onReopen={reopenAnnotation}
          />
        )}
        {rightPanel === "activity" && (
          <Activity events={events} nodes={canvas.nodes}
                    onFocus={(id) => setFocus({ id, nonce: Date.now() })} />
        )}
      </div>

      {draft && draftCard && (
        <AnnotationComposer
          draft={draft}
          cardTitle={draftCard.sp_title || draftCard.id}
          onCancel={() => setDraft(null)}
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
            <iframe sandbox="allow-scripts" srcDoc={popOut.text ?? ""} title={popOut.sp_title ?? popOut.id} />
          </div>
        </div>
      )}

      {toast && <div className="toast">{toast}</div>}
      <footer className="hints">
        {annotateMode ? (
          <>click a card to pin a comment · <strong>shift-drag on a card to comment on a
          region</strong> · esc to stop</>
        ) : (
          <>drag to pan · ⌘/ctrl-scroll to zoom · scroll over a card to scroll the card ·
          double-click empty space for a card · drag the ◇ handle to link ·
          double-click a card to edit · c to comment</>
        )}
      </footer>
    </div>
  );
}
