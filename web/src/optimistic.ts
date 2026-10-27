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
 */
export type PendingWrite =
  | { op: "create"; annotation: Annotation }
  | { op: "resolve"; id: string; reply: string | null }
  | { op: "reopen"; id: string };

/** Optimistic comments carry this id prefix until the server names them. */
export const PENDING_PREFIX = "pending:";

export const isPending = (annotation: Annotation) => annotation.id.startsWith(PENDING_PREFIX);

export function withPending(server: Annotation[], pending: PendingWrite[]): Annotation[] {
  if (pending.length === 0) return server;
  let shown = server;
  for (const write of pending) {
    if (write.op === "create") {
      shown = [...shown, write.annotation];
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
