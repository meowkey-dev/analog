// Types and fetch wrappers for contracts/openapi.json.
// Same-origin: dev proxies /api to 127.0.0.1:8787, prod is served by the server.

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

// The UI always writes as the human (SPEC §2.2).
const ACTOR = { actor: "human", actor_kind: "human" };

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

function url(path: string, params: Record<string, unknown> = {}): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null) query.set(key, String(value));
  }
  const suffix = query.toString();
  return `/api${path}${suffix ? `?${suffix}` : ""}`;
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
    headers: body === undefined ? headers : { "content-type": "application/json", ...headers },
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
  listSpaces: () => request<Space[]>("GET", "/spaces"),

  createSpace: (slug: string, title: string, revision_mode: "replace" | "branch" = "replace") =>
    request<Space>("POST", "/spaces", ACTOR, { slug, title, revision_mode }),

  getSpace: (slug: string) => request<Space>("GET", `/spaces/${slug}`),

  getCanvas: (slug: string, includeDeleted = false) =>
    request<Canvas>("GET", `/spaces/${slug}/canvas`, { include_deleted: includeDeleted }),

  createCards: (slug: string, cards: Array<Partial<Node> & { title?: string; content?: string; kind?: Kind }>) =>
    request<Node[]>("POST", `/spaces/${slug}/cards`, ACTOR, { cards }),

  updateCard: (slug: string, id: string, patch: Partial<Node>, ifMatch?: number) =>
    request<Node>("PATCH", `/spaces/${slug}/cards/${id}`, ACTOR, patch,
      ifMatch === undefined ? {} : { "If-Match": String(ifMatch) }),

  deleteCard: (slug: string, id: string) =>
    request<void>("DELETE", `/spaces/${slug}/cards/${id}`, ACTOR),

  createLink: (slug: string, fromNode: string, toNode: string, label?: string, sides?: { fromSide?: Side; toSide?: Side }) =>
    request<Edge[]>("POST", `/spaces/${slug}/links`, ACTOR, {
      edges: [{ fromNode, toNode, label, ...sides }],
    }).then((edges) => edges[0]!),

  deleteLink: (slug: string, id: string) =>
    request<void>("DELETE", `/spaces/${slug}/links/${id}`, ACTOR),

  listAnnotations: (slug: string, resolved?: boolean) =>
    request<Annotation[]>("GET", `/spaces/${slug}/annotations`, { resolved }),

  createAnnotation: (slug: string, card_id: string, body: string, selector: Selector, motivation: Motivation) =>
    request<Annotation>("POST", `/spaces/${slug}/annotations`, ACTOR, {
      card_id, body, selector, motivation,
    }),

  resolveAnnotation: (slug: string, id: string, reply?: string, resolved = true) =>
    request<Annotation>("PATCH", `/spaces/${slug}/annotations/${id}`, ACTOR, { resolved, reply }),

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
  let source: EventSource | null = null;
  let pollTimer: number | undefined;

  const poll = async () => {
    if (closed) return;
    try {
      const page = await api.listEvents(slug, cursor);
      for (const event of page.events) {
        cursor = Math.max(cursor, event.seq);
        onEvent(event);
      }
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

  const connect = () => {
    if (closed) return;
    source = new EventSource(url(`/spaces/${slug}/events/stream`, { since: cursor }));
    source.onopen = () => {
      onStatus?.("live");
      if (pollTimer !== undefined) {
        window.clearTimeout(pollTimer);
        pollTimer = undefined;
      }
    };
    source.onmessage = (message) => {
      const event = JSON.parse(message.data) as AnalogEvent;
      cursor = Math.max(cursor, event.seq);
      onEvent(event);
    };
    source.onerror = () => {
      // EventSource retries on its own; polling covers the gap meanwhile.
      startPolling();
    };
  };

  // Named SSE events (event: card.created) do not fire onmessage, so listen per type.
  const originalConnect = connect;
  const connectWithTypes = () => {
    originalConnect();
    if (!source) return;
    const types: EventType[] = [
      "card.created", "card.updated", "card.moved", "card.deleted",
      "link.created", "link.deleted", "annotation.created", "annotation.resolved",
    ];
    for (const type of types) {
      source.addEventListener(type, (message) => {
        const event = JSON.parse((message as MessageEvent).data) as AnalogEvent;
        cursor = Math.max(cursor, event.seq);
        onEvent(event);
      });
    }
  };

  connectWithTypes();

  return () => {
    closed = true;
    source?.close();
    if (pollTimer !== undefined) window.clearTimeout(pollTimer);
  };
}
