import type { AnalogEvent, Node } from "./api";

/**
 * What this browser last saw of each card, so the UI can say "changed since you
 * last looked" and show the diff.
 *
 * The server keeps no text history: a replace-mode edit overwrites the card and
 * the event log carries no payload for it (SPEC §2.4, §9 lists version history
 * as future work). The one place the old text still exists is a browser that
 * rendered it, so the snapshot lives here, in localStorage, per server and space.
 * It is a convenience, never a source of truth: another browser, a cleared
 * profile or a card too large to keep simply has no diff to offer.
 */

export interface CardSnapshot {
  /** Hash of title, text and file as last acknowledged. */
  h: string;
  /** The acknowledged text, when small enough to keep. */
  t?: string;
  /** The text acknowledged before that one: the "last change" diff. */
  p?: string;
  /** Event seq at acknowledgement; comments after it are new. */
  s: number;
}

export interface SeenRecord {
  seq: number;
  cards: Record<string, CardSnapshot>;
}

export type ChangeKind = "new" | "edited" | "comments";

export interface CardChange {
  kind: ChangeKind;
  /** The text last seen, when the change is an edit and it was kept. */
  before?: string;
  /** Comments from someone else since the card was last seen. */
  newComments: number;
  /** Who made the most recent change, when the event log says. */
  actor?: string;
}

/** One line for a change note: "edited by claude-code · 2 new comments". */
export function describeChange(change: CardChange): string {
  const by = change.actor ? ` by ${change.actor}` : "";
  const comments = change.newComments === 1 ? "1 new comment" : `${change.newComments} new comments`;
  if (change.kind === "comments") return comments;
  const head = change.kind === "new" ? `new${by}` : `edited${by} since you last looked`;
  return change.newComments > 0 ? `${head} · ${comments}` : head;
}

/** Texts past this are hashed but not kept; a diff of them is unreadable anyway. */
export const MAX_KEPT = 60_000;

/** FNV-1a, 32-bit. Collisions only cost a missed badge. */
export function hash(value: string): string {
  let h = 0x811c9dc5;
  for (let i = 0; i < value.length; i++) {
    h ^= value.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0).toString(36) + value.length.toString(36);
}

export function fingerprint(node: Node): string {
  return hash(`${node.sp_title ?? ""}\u0000${node.text ?? ""}\u0000${node.file ?? ""}`);
}

function keep(text: string | undefined): string | undefined {
  return text !== undefined && text.length <= MAX_KEPT ? text : undefined;
}

export function snapshotOf(node: Node, seq: number, previous?: CardSnapshot): CardSnapshot {
  const h = fingerprint(node);
  const snap: CardSnapshot = { h, s: seq };
  const t = keep(node.text);
  if (t !== undefined) snap.t = t;
  if (previous && previous.h !== h) {
    if (previous.t !== undefined) snap.p = previous.t;
  } else if (previous?.p !== undefined) {
    snap.p = previous.p;
  }
  return snap;
}

/** Everything on the board as already seen: the first visit has nothing new. */
export function baseline(nodes: Node[], seq: number): SeenRecord {
  const cards: Record<string, CardSnapshot> = {};
  for (const node of nodes) cards[node.id] = snapshotOf(node, seq);
  return { seq, cards };
}

export function markSeen(record: SeenRecord, nodes: Node[], seq: number): SeenRecord {
  const cards = { ...record.cards };
  for (const node of nodes) cards[node.id] = snapshotOf(node, seq, cards[node.id]);
  return { seq: Math.max(record.seq, seq), cards };
}

/** Every card acknowledged, and snapshots of cards no longer on the board dropped. */
export function markAllSeen(record: SeenRecord, nodes: Node[], seq: number): SeenRecord {
  const cards: Record<string, CardSnapshot> = {};
  for (const node of nodes) cards[node.id] = snapshotOf(node, seq, record.cards[node.id]);
  return { seq: Math.max(record.seq, seq), cards };
}

/** The card a comment event is about. */
function commentCard(event: AnalogEvent): string | undefined {
  return event.type === "annotation.created" ? event.payload?.card_id : undefined;
}

/**
 * Cards that differ from what this browser last acknowledged. `me` is excluded
 * from comment counts: your own comment is not news to you.
 */
export function changesSince(
  record: SeenRecord,
  nodes: Node[],
  events: AnalogEvent[],
  me: string,
): Map<string, CardChange> {
  const lastActor = new Map<string, string>();
  const comments = new Map<string, AnalogEvent[]>();
  for (const event of events) {
    if (event.type === "card.created" || event.type === "card.updated") {
      lastActor.set(event.subject_id, event.actor);
    }
    const card = commentCard(event);
    if (card && event.actor !== me) {
      const list = comments.get(card);
      if (list) list.push(event);
      else comments.set(card, [event]);
    }
  }

  const changes = new Map<string, CardChange>();
  for (const node of nodes) {
    const snap = record.cards[node.id];
    const since = snap?.s ?? record.seq;
    const newComments = (comments.get(node.id) ?? []).filter((e) => e.seq > since).length;
    const actor = lastActor.get(node.id);
    if (!snap) {
      changes.set(node.id, { kind: "new", newComments, actor });
    } else if (snap.h !== fingerprint(node)) {
      changes.set(node.id, { kind: "edited", before: snap.t, newComments, actor });
    } else if (newComments > 0) {
      changes.set(node.id, { kind: "comments", newComments });
    }
  }
  return changes;
}

/** The text before the most recent change this browser acknowledged, if kept. */
export function lastChange(record: SeenRecord, node: Node): string | undefined {
  const snap = record.cards[node.id];
  if (!snap || snap.h !== fingerprint(node)) return undefined;
  return snap.p;
}

// --- storage -----------------------------------------------------------------

export function storageKey(server: string, slug: string): string {
  return `analog.seen.v1:${server}:${slug}`;
}

export function loadRecord(key: string): SeenRecord | null {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as SeenRecord;
    return typeof parsed?.seq === "number" && parsed.cards ? parsed : null;
  } catch {
    return null;
  }
}

/**
 * Past the quota, keep the hashes and drop the texts: losing diffs is better
 * than losing the "changed" badges too.
 */
export function saveRecord(key: string, record: SeenRecord): void {
  try {
    localStorage.setItem(key, JSON.stringify(record));
    return;
  } catch {
    // fall through to the lean copy
  }
  try {
    const cards: Record<string, CardSnapshot> = {};
    for (const [id, snap] of Object.entries(record.cards)) cards[id] = { h: snap.h, s: snap.s };
    localStorage.setItem(key, JSON.stringify({ seq: record.seq, cards }));
  } catch {
    // storage unavailable: badges last for this tab only
  }
}
