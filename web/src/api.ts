// Types and fetch wrappers for contracts/openapi.json.
//
// The server may be same-origin (it serves this bundle, and the dev server proxies
// /api to 127.0.0.1:8787) or remote (the Tauri shell, or any browser pointed at
// another host). Both are the same code path: a base URL and an optional per-actor
// bearer token, held in connection.ts.
import { loadConnection, type Connection } from "./connection";

export type ActorKind = "human" | "agent";
export type Kind = "md" | "html" | "svg" | "plain";
export type Motivation = "commenting" | "assessing" | "editing";
export type Side = "top" | "right" | "bottom" | "left";

export interface Node {
  id: string;
  type: "text" | "file" | "link" | "group";
  x: number;
  y: number;
  width: number;
  height: number;
  color?: string;
  text?: string;
  file?: string;
  sp_kind?: Kind;
  sp_title?: string;
  sp_created_by?: string;
  sp_rev?: number;
  sp_superseded_by?: string;
  sp_deleted_at?: string;
  sp_meta?: Record<string, unknown>;
}

export interface Edge {
  id: string;
  fromNode: string;
  fromSide?: Side;
  toNode: string;
  toSide?: Side;
  label?: string;
  color?: string;
  sp_created_by?: string;
}

export interface Canvas {
  nodes: Node[];
  edges: Edge[];
}

export interface Space {
  id: string;
  slug: string;
  title: string;
  revision_mode: "replace" | "branch";
  seq: number;
  created_at: string;
  counts?: { cards: number; links: number; open_annotations: number };
}

export type Selector =
  | null
  | { type: "point"; x: number; y: number }
  | { type: "rect"; x: number; y: number; w: number; h: number };

export interface Annotation {
  id: string;
  card_id: string;
  card_title?: string;
  /** Branch mode only: the card that replaced this one. Absent while it is current. */
  card_superseded_by?: string;
  card_rev: number;
  selector: Selector;
  body: string;
  motivation: Motivation;
  creator: string;
  creator_kind: ActorKind;
  resolved: boolean;
  resolved_reply: string | null;
  stale: boolean;
  created_at: string;
}

export type EventType =
  | "space.created" | "space.deleted"
  | "card.created" | "card.updated" | "card.moved" | "card.deleted"
  | "link.created" | "link.deleted"
  | "annotation.created" | "annotation.resolved";

export interface AnalogEvent {
  seq: number;
  ts: string;
  type: EventType;
  subject_id: string;
  actor: string;
  actor_kind: ActorKind;
  payload?: Record<string, any>;
}

export interface Health {
  ok: true;
  service: "analog";
  version: string;
  auth_required: boolean;
}

export interface Whoami {
  authenticated: boolean;
  actor: string | null;
  actor_kind: ActorKind | null;
}

let connection: Connection = loadConnection();

/** Who this UI writes as. "human" until a token says otherwise (SPEC §2.2). */
let identity = { actor: "human", actor_kind: "human" as ActorKind };

export function setConnection(next: Connection): void {
  connection = next;
}

export function getConnection(): Connection {
  return connection;
}

export function setIdentity(actor: string, actor_kind: ActorKind): void {
  identity = { actor, actor_kind };
}

export function getIdentity(): { actor: string; actor_kind: ActorKind } {
  return identity;
}

/** actor/actor_kind on every mutation, and they must match the token (0.3.0). */
function ACTOR_PARAMS(): Record<string, string> {
  return { actor: identity.actor, actor_kind: identity.actor_kind };
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly body: any,
  ) {
    super(message);
  }
}

function url(path: string, params: Record<string, unknown> = {}, base = connection.baseUrl): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null) query.set(key, String(value));
  }
  const suffix = query.toString();
  return `${base}/api${path}${suffix ? `?${suffix}` : ""}`;
}

/** Absolute URL for a path the server handed us, e.g. a file node's `file`. */
export function resolveUrl(path: string): string {
  if (/^https?:\/\//i.test(path)) return path;
  return `${connection.baseUrl}${path}`;
}

function authHeaders(token: string | null = connection.token): Record<string, string> {
  return token ? { authorization: `Bearer ${token}` } : {};
}

async function request<T>(
  method: string,
  path: string,
  params: Record<string, unknown> = {},
  body?: unknown,
  headers: Record<string, string> = {},
): Promise<T> {
  const response = await fetch(url(path, params), {
    method,
    headers: {
      ...authHeaders(),
      ...(body === undefined ? {} : { "content-type": "application/json" }),
      ...headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    let payload: any = null;
    try {
      payload = await response.json();
    } catch {
      /* non-JSON error body */
    }
    throw new ApiError(
      response.status,
      payload?.error ?? "error",
      payload?.message ?? response.statusText,
      payload,
    );
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

export const api = {
  /** Probe any server, with any token, without changing the current connection. */
  probe: async (baseUrl: string, token: string | null): Promise<Health> => {
    const response = await fetch(url("/health", {}, baseUrl), { headers: authHeaders(token) });
    if (!response.ok) throw new ApiError(response.status, "error", response.statusText, null);
    return (await response.json()) as Health;
  },

  probeWhoami: async (baseUrl: string, token: string | null): Promise<Whoami> => {
    const response = await fetch(url("/whoami", {}, baseUrl), { headers: authHeaders(token) });
    if (!response.ok) {
      let payload: any = null;
      try { payload = await response.json(); } catch { /* non-JSON */ }
      throw new ApiError(response.status, payload?.error ?? "error",
        payload?.message ?? response.statusText, payload);
    }
    return (await response.json()) as Whoami;
  },

  health: () => request<Health>("GET", "/health"),

  whoami: () => request<Whoami>("GET", "/whoami"),

  /** Media cannot be an <img src>: an image request carries no Authorization
   *  header. Fetch it and hand back an object URL the caller must revoke. */
  mediaObjectUrl: async (path: string): Promise<string> => {
    const response = await fetch(resolveUrl(path), { headers: authHeaders() });
    if (!response.ok) throw new ApiError(response.status, "error", response.statusText, null);
    return URL.createObjectURL(await response.blob());
  },

  listSpaces: () => request<Space[]>("GET", "/spaces"),

  createSpace: (slug: string, title: string, revision_mode: "replace" | "branch" = "replace") =>
    request<Space>("POST", "/spaces", ACTOR_PARAMS(), { slug, title, revision_mode }),

  getSpace: (slug: string) => request<Space>("GET", `/spaces/${slug}`),

  getCanvas: (slug: string, includeDeleted = false) =>
    request<Canvas>("GET", `/spaces/${slug}/canvas`, { include_deleted: includeDeleted }),

  createCards: (slug: string, cards: Array<Partial<Node> & { title?: string; content?: string; kind?: Kind }>) =>
    request<Node[]>("POST", `/spaces/${slug}/cards`, ACTOR_PARAMS(), { cards }),

  updateCard: (slug: string, id: string, patch: Partial<Node>, ifMatch?: number) =>
    request<Node>("PATCH", `/spaces/${slug}/cards/${id}`, ACTOR_PARAMS(), patch,
      ifMatch === undefined ? {} : { "If-Match": String(ifMatch) }),

  deleteCard: (slug: string, id: string) =>
    request<void>("DELETE", `/spaces/${slug}/cards/${id}`, ACTOR_PARAMS()),

  createLink: (slug: string, fromNode: string, toNode: string, label?: string, sides?: { fromSide?: Side; toSide?: Side }) =>
    request<Edge[]>("POST", `/spaces/${slug}/links`, ACTOR_PARAMS(), {
      edges: [{ fromNode, toNode, label, ...sides }],
    }).then((edges) => edges[0]!),

  deleteLink: (slug: string, id: string) =>
    request<void>("DELETE", `/spaces/${slug}/links/${id}`, ACTOR_PARAMS()),

  listAnnotations: (slug: string, resolved?: boolean) =>
    request<Annotation[]>("GET", `/spaces/${slug}/annotations`, { resolved }),

  createAnnotation: (slug: string, card_id: string, body: string, selector: Selector, motivation: Motivation) =>
    request<Annotation>("POST", `/spaces/${slug}/annotations`, ACTOR_PARAMS(), {
      card_id, body, selector, motivation,
    }),

  resolveAnnotation: (slug: string, id: string, reply?: string, resolved = true) =>
    request<Annotation>("PATCH", `/spaces/${slug}/annotations/${id}`, ACTOR_PARAMS(), { resolved, reply }),

  listEvents: (slug: string, since = 0, limit = 500) =>
    request<{ events: AnalogEvent[]; cursor: number }>("GET", `/spaces/${slug}/events`, { since, limit }),
};

/**
 * Subscribe to the event stream, falling back to 2s polling if it drops (SPEC §5).
 * Returns an unsubscribe function.
 */
export function subscribe(
  slug: string,
  since: number,
  onEvent: (event: AnalogEvent) => void,
  onStatus?: (status: "live" | "polling") => void,
): () => void {
  let cursor = since;
  let closed = false;
  let controller: AbortController | null = null;
  let pollTimer: number | undefined;

  const deliver = (event: AnalogEvent) => {
    cursor = Math.max(cursor, event.seq);
    onEvent(event);
  };

  const poll = async () => {
    if (closed) return;
    try {
      const page = await api.listEvents(slug, cursor);
      page.events.forEach(deliver);
    } catch {
      /* keep polling: the server may be restarting */
    }
    if (!closed) pollTimer = window.setTimeout(poll, 2000);
  };

  const startPolling = () => {
    if (closed || pollTimer !== undefined) return;
    onStatus?.("polling");
    pollTimer = window.setTimeout(poll, 2000);
  };

  const stopPolling = () => {
    if (pollTimer !== undefined) {
      window.clearTimeout(pollTimer);
      pollTimer = undefined;
    }
  };

  /**
   * EventSource cannot set an Authorization header, and a token in the query
   * string would leak into logs and referrers. So the stream is read off fetch,
   * and SSE framing is parsed here — about fifteen lines, and it also gives us
   * honest control over reconnection.
   */
  const connect = async () => {
    while (!closed) {
      controller = new AbortController();
      try {
        const response = await fetch(url(`/spaces/${slug}/events/stream`), {
          headers: {
            accept: "text/event-stream",
            "Last-Event-ID": String(cursor),
            ...authHeaders(),
          },
          signal: controller.signal,
        });
        if (!response.ok || !response.body) throw new Error(`stream ${response.status}`);

        onStatus?.("live");
        stopPolling();

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          let split: number;
          while ((split = buffer.indexOf("\n\n")) >= 0) {
            const frame = buffer.slice(0, split);
            buffer = buffer.slice(split + 2);
            const data = frame
              .split("\n")
              .filter((line) => line.startsWith("data:"))
              .map((line) => line.slice(5).trim())
              .join("\n");
            if (!data) continue;              // a comment frame, i.e. a keepalive
            try {
              deliver(JSON.parse(data) as AnalogEvent);
            } catch {
              /* a partial or malformed frame is not worth dropping the stream for */
            }
          }
        }
      } catch {
        /* fall through to the backoff below */
      }
      if (closed) return;
      startPolling();                          // cover the gap while we reconnect
      await new Promise((resolve) => window.setTimeout(resolve, 2000));
    }
  };

  void connect();

  return () => {
    closed = true;
    controller?.abort();
    stopPolling();
  };
}
