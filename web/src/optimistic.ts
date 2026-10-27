import type { Annotation } from "./api";

/**
 * Comment writes the UI shows before the server has confirmed them (#127).
 *
 * The server's list stays the source of truth; pending writes are a layer over
 * it, applied at render. A write leaves the layer when its request settles: on
 * success the server's answer is merged into the list, so nothing jumps; on
 * failure it is simply dropped, which is the rollback. The response, not the
 * SSE event, is what confirms: a reopen emits no event at all. Applying a
 * write the list already reflects changes nothing, so an SSE refresh that
 * lands first is harmless.
 *
 * Each write belongs to the space it was made in, and only shows there.
 */
export type PendingWrite = { slug: string } & (
  | {
      op: "create";
      annotation: Annotation;
      /** The server's comment ids when the write began: a new one may be this. */
      known: ReadonlySet<string>;
    }
  | { op: "resolve"; id: string; reply: string | null }
  | { op: "reopen"; id: string }
);

/** Whether a listed comment could be what a pending create became. */
const becameThis = (listed: Annotation, shown: Annotation) =>
  listed.card_id === shown.card_id && listed.body === shown.body &&
  listed.creator === shown.creator && listed.motivation === shown.motivation;

/** Optimistic comments carry this id prefix until the server names them. */
export const PENDING_PREFIX = "pending:";

export const isPending = (annotation: Annotation) => annotation.id.startsWith(PENDING_PREFIX);

export function withPending(server: Annotation[], pending: PendingWrite[], slug: string): Annotation[] {
  const writes = pending.filter((w) => w.slug === slug);
  if (writes.length === 0) return server;
  let shown = server;
  // A refresh can list a new comment before its POST answers. That comment
  // stands in for the pending copy; each one stands in for only one.
  const claimed = new Set<string>();
  for (const write of writes) {
    if (write.op === "create") {
      const listed = server.find((a) => !write.known.has(a.id) && !claimed.has(a.id) &&
        becameThis(a, write.annotation));
      if (listed) claimed.add(listed.id);
      else shown = [...shown, write.annotation];
      continue;
    }
    const resolved = write.op === "resolve";
    shown = shown.map((a) => (a.id !== write.id ? a : {
      ...a,
      resolved,
      // Reopening clears the reply on the server too.
      resolved_reply: resolved ? write.reply : null,
    }));
  }
  return shown;
}

/** The server's copy of one comment replaces the listed one, or joins the list. */
export function confirmed(server: Annotation[], annotation: Annotation): Annotation[] {
  return server.some((a) => a.id === annotation.id)
    ? server.map((a) => (a.id === annotation.id ? annotation : a))
    : [...server, annotation];
}

/**
 * Runs `work` after every earlier write to the same comment has settled, so
 * the server applies them, and their answers land, in the order they were
 * made: a resolve answered after a later reopen would otherwise win.
 */
export function inOrder<T>(queues: Map<string, Promise<unknown>>, key: string, work: () => Promise<T>): Promise<T> {
  const run = (queues.get(key) ?? Promise.resolve()).then(work);
  const tail = run.catch(() => undefined);
  queues.set(key, tail);
  void tail.then(() => { if (queues.get(key) === tail) queues.delete(key); });
  return run;
}
