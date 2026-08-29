/**
 * Which server this UI talks to.
 *
 * The same bundle ships two ways: served by the server itself (same origin, base
 * `""`), and inside the Tauri shell, where there is no origin to inherit and the
 * server must be named. So the base URL is data, not a build constant.
 */

export interface Connection {
  /** Origin of the server, or "" for same-origin. Never includes /api. */
  baseUrl: string;
  token: string | null;
  label?: string;
}

const KEY = "analog.connection";
const RECENT_KEY = "analog.recent";

export const SAME_ORIGIN: Connection = { baseUrl: "", token: null };

/** Where a shell with no origin of its own should look first. */
export const DEFAULT_REMOTE: Connection = { baseUrl: "http://127.0.0.1:8787", token: null };

/** True in the Tauri shell, false in any browser. */
export function servedOverHttp(): boolean {
  return window.location.protocol === "http:" || window.location.protocol === "https:";
}

/** Strip trailing slashes and a trailing /api, so both forms of paste work. */
export function normalizeBase(raw: string): string {
  const trimmed = raw.trim().replace(/\/+$/, "");
  if (!trimmed) return "";
  const withScheme = /^https?:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`;
  return withScheme.replace(/\/api$/i, "");
}

function read<T>(key: string, fallback: T): T {
  try {
    const raw = window.localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    // Private windows and blocked site data both throw here.
    return fallback;
  }
}

function write(key: string, value: unknown): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* nothing to do; the session just won't be remembered */
  }
}

export function loadConnection(): Connection {
  const stored = read<Partial<Connection> | null>(KEY, null);
  // A browser served by the server already knows where home is. The desktop shell
  // is loaded from tauri://, where "same origin" points at the bundle itself.
  if (!stored) return servedOverHttp() ? SAME_ORIGIN : DEFAULT_REMOTE;
  return {
    baseUrl: normalizeBase(stored.baseUrl ?? ""),
    token: stored.token ?? null,
    label: stored.label,
  };
}

export function saveConnection(connection: Connection): void {
  write(KEY, connection);
  if (!connection.baseUrl) return;
  const recent = loadRecent().filter((c) => c.baseUrl !== connection.baseUrl);
  write(RECENT_KEY, [connection, ...recent].slice(0, 8));
}

export function loadRecent(): Connection[] {
  return read<Connection[]>(RECENT_KEY, []).filter((c) => typeof c?.baseUrl === "string");
}

export function forgetRecent(baseUrl: string): void {
  write(RECENT_KEY, loadRecent().filter((c) => c.baseUrl !== baseUrl));
}

export function clearConnection(): void {
  try {
    window.localStorage.removeItem(KEY);
  } catch {
    /* ignore */
  }
}

/** What to show as the server's name. */
export function describe(connection: Connection): string {
  if (!connection.baseUrl) return window.location.host || "this server";
  try {
    return new URL(connection.baseUrl).host;
  } catch {
    return connection.baseUrl;
  }
}
