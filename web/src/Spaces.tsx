import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "./api";
import type { Space } from "./api";

/**
 * Space selection. A Space is the unit of work (SPEC §1: one space = one
 * workstream = one agent thread), so this is the app's front door: without it the
 * only way into a canvas is to already know its slug.
 */

/** [a-z0-9-]{1,64}, the pattern the API enforces. */
export function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64);
}

function useSpaces() {
  const [spaces, setSpaces] = useState<Space[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = async () => {
    try {
      setSpaces(await api.listSpaces());
      setError(null);
    } catch (exc) {
      setError(exc instanceof ApiError ? exc.message : String(exc));
    }
  };

  useEffect(() => {
    void load();
  }, []);

  return { spaces, error, reload: load };
}

function counts(space: Space) {
  const c = space.counts ?? { cards: 0, links: 0, open_annotations: 0 };
  return c;
}

export function SpaceIndex({ onOpen }: { onOpen: (slug: string) => void }) {
  const { spaces, error, reload } = useSpaces();
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [mode, setMode] = useState<"replace" | "branch">("replace");
  const [creating, setCreating] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  const effectiveSlug = slug || slugify(title);

  const create = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!effectiveSlug) return;
    setCreating(true);
    setProblem(null);
    try {
      const space = await api.createSpace(effectiveSlug, title || effectiveSlug, mode);
      onOpen(space.slug);
    } catch (exc) {
      setProblem(exc instanceof ApiError
        ? (exc.code === "conflict" ? `A space called "${effectiveSlug}" already exists.` : exc.message)
        : String(exc));
      await reload();
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="index">
      <header>
        <h1>analog</h1>
        <p>A shared canvas for you and your agents. One space per workstream.</p>
      </header>

      {error && (
        <div className="index-error">
          <p>Can't reach the API: {error}</p>
          <p className="hint">
            Start it with <code>analog-server</code>, or open{" "}
            <a href="/s/redesign?fixture">the fixture space</a>, which needs no database.
          </p>
        </div>
      )}

      {spaces && spaces.length === 0 && (
        <p className="empty">No spaces yet. Make one below, or from a shell with
          {" "}<code>analog new &lt;slug&gt;</code>.</p>
      )}

      {spaces && spaces.length > 0 && (
        <ul className="space-list">
          {spaces.map((space) => {
            const c = counts(space);
            return (
              <li key={space.id}>
                <a href={`/s/${space.slug}`}
                   onClick={(event) => { event.preventDefault(); onOpen(space.slug); }}>
                  <span className="space-title">{space.title}</span>
                  <span className="space-slug">/{space.slug}</span>
                  <span className="space-counts">
                    {c.cards} card{c.cards === 1 ? "" : "s"} · {c.links} link{c.links === 1 ? "" : "s"}
                  </span>
                  {c.open_annotations > 0 && (
                    <span className="badge comments" title="open comments">
                      {c.open_annotations} open
                    </span>
                  )}
                  {space.revision_mode === "branch" && <span className="badge">branch</span>}
                </a>
              </li>
            );
          })}
        </ul>
      )}

      <form className="new-space" onSubmit={create}>
        <h2>New space</h2>
        <div className="row">
          <input placeholder="Title, e.g. Nav redesign" value={title}
                 onChange={(event) => setTitle(event.target.value)} />
          <input placeholder={effectiveSlug || "slug"} value={slug}
                 onChange={(event) => setSlug(slugify(event.target.value))} />
          <select value={mode} onChange={(event) => setMode(event.target.value as "replace" | "branch")}>
            <option value="replace">replace — rewrite cards in place</option>
            <option value="branch">branch — keep superseded cards</option>
          </select>
          <button type="submit" disabled={!effectiveSlug || creating}>Create</button>
        </div>
        {effectiveSlug && <p className="hint">Opens at /s/{effectiveSlug}</p>}
        {problem && <p className="problem">{problem}</p>}
      </form>
    </div>
  );
}

/** The topbar space name, which opens a list of the others. */
export function SpaceSwitcher({ current, title, onOpen }: {
  current: string;
  title: string;
  onOpen: (slug: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [spaces, setSpaces] = useState<Space[] | null>(null);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    api.listSpaces().then(setSpaces).catch(() => setSpaces([]));
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

  return (
    <div className="switcher" ref={box}>
      <button className="switcher-button" onClick={() => setOpen((o) => !o)}
              title="Switch space">
        {title}
        <span className="slug">/{current}</span>
        <span className="caret">▾</span>
      </button>
      {open && (
        <div className="switcher-menu">
          {spaces === null && <div className="switcher-row muted">loading…</div>}
          {spaces?.map((space) => {
            const isCurrent = space.slug === current;
            return (
              <button key={space.id}
                      className={`switcher-row${isCurrent ? " current" : ""}`}
                      aria-current={isCurrent ? "true" : undefined}
                      onClick={() => { setOpen(false); if (!isCurrent) onOpen(space.slug); }}>
                <span className="tick">{isCurrent ? "✓" : ""}</span>
                <span>{space.title}</span>
                <span className="slug">/{space.slug}</span>
                {(space.counts?.open_annotations ?? 0) > 0 && (
                  <span className="badge comments">{space.counts!.open_annotations}</span>
                )}
              </button>
            );
          })}
          <a className="switcher-row all" href="/"
             onClick={(event) => { event.preventDefault(); setOpen(false); onOpen(""); }}>
            all spaces…
          </a>
        </div>
      )}
    </div>
  );
}
