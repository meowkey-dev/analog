import { useEffect, useState } from "react";
import { api, ApiError, setConnection, setIdentity } from "./api";
import type { Health } from "./api";
import {
  describe, forgetRecent, loadRecent, normalizeBase, saveConnection, servedOverHttp,
  type Connection,
} from "./connection";

/**
 * Choosing a server.
 *
 * Served from the server itself this is usually skipped entirely — same origin, no
 * token. It exists for the two cases the original one-route app could not express:
 * a remote server that wants a token, and the Tauri shell, which has no origin to
 * inherit and must be told where to look.
 */

export interface Connected {
  connection: Connection;
  health: Health;
  actor: string;
  actorKind: "human" | "agent";
}

/** Probe a server and settle on an identity, or explain why not. */
export async function attempt(connection: Connection): Promise<Connected> {
  let health: Health;
  try {
    health = await api.probe(connection.baseUrl, connection.token);
  } catch (exc) {
    throw new Error(
      exc instanceof ApiError && exc.status
        ? `${describe(connection)} answered ${exc.status}. Is that an Analog server?`
        : `Could not reach ${describe(connection) || "the server"}. Is it running?`,
    );
  }

  if (!health.auth_required) {
    return { connection, health, actor: "human", actorKind: "human" };
  }
  if (!connection.token) {
    throw new Error(`${describe(connection)} requires a token. Ask for one with `
      + `\`analog token add <you> --kind human\` on the server.`);
  }

  let who;
  try {
    who = await api.probeWhoami(connection.baseUrl, connection.token);
  } catch (exc) {
    if (exc instanceof ApiError && exc.status === 401) {
      throw new Error("That token was rejected. It may have been revoked or reissued.");
    }
    throw exc;
  }
  if (!who.authenticated || !who.actor || !who.actor_kind) {
    throw new Error("That token was rejected.");
  }
  return { connection, health, actor: who.actor, actorKind: who.actor_kind };
}

/** Apply a successful attempt to the module-level client state. */
export function adopt(result: Connected): void {
  setConnection(result.connection);
  setIdentity(result.actor, result.actorKind);
}

export function Connect(props: {
  initial: Connection;
  problem?: string | null;
  onConnected: (result: Connected) => void;
}) {
  const [baseUrl, setBaseUrl] = useState(props.initial.baseUrl);
  const [token, setToken] = useState(props.initial.token ?? "");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(props.problem ?? null);
  const [recent, setRecent] = useState(loadRecent());
  // Only offer "this server" when the page came from one — in Tauri it did not.
  const sameOriginPossible = servedOverHttp();

  useEffect(() => setProblem(props.problem ?? null), [props.problem]);

  const connect = async (candidate: Connection) => {
    setBusy(true);
    setProblem(null);
    try {
      const result = await attempt(candidate);
      adopt(result);
      saveConnection(result.connection);
      setRecent(loadRecent());
      props.onConnected(result);
    } catch (exc) {
      setProblem(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="index connect">
      <header>
        <h1>analog</h1>
        <p>Connect to a server. It can be this machine or another one.</p>
      </header>

      {problem && <div className="index-error"><p>{problem}</p></div>}

      <form
        className="new-space"
        onSubmit={(event) => {
          event.preventDefault();
          void connect({ baseUrl: normalizeBase(baseUrl), token: token.trim() || null });
        }}
      >
        <h2>Server</h2>
        <div className="row">
          <input
            autoFocus
            placeholder="http://127.0.0.1:8787 — or leave empty for this server"
            value={baseUrl}
            onChange={(event) => setBaseUrl(event.target.value)}
          />
        </div>
        <div className="row">
          <input
            type="password"
            placeholder="token (only if the server asks for one)"
            value={token}
            onChange={(event) => setToken(event.target.value)}
          />
          <button type="submit" disabled={busy}>{busy ? "connecting…" : "Connect"}</button>
        </div>
        <p className="hint">
          A token identifies one actor. The server issues them with{" "}
          <code>analog token add &lt;name&gt; --kind human</code>, and it decides who
          you write as — the name cannot be claimed from this end.
        </p>
      </form>

      {sameOriginPossible && baseUrl !== "" && (
        <p className="hint">
          <a href="#" onClick={(event) => { event.preventDefault(); void connect({ baseUrl: "", token: null }); }}>
            Use the server that served this page
          </a>{" "}({window.location.host})
        </p>
      )}

      {recent.length > 0 && (
        <>
          <h2 className="recent-heading">Recent</h2>
          <ul className="space-list">
            {recent.map((entry) => (
              <li key={entry.baseUrl}>
                <a
                  href={entry.baseUrl}
                  onClick={(event) => { event.preventDefault(); void connect(entry); }}
                >
                  <span className="space-title">{describe(entry)}</span>
                  <span className="space-slug">{entry.baseUrl}</span>
                  {entry.token && <span className="badge">token</span>}
                  <span
                    className="space-counts forget"
                    onClick={(event) => {
                      event.preventDefault();
                      event.stopPropagation();
                      forgetRecent(entry.baseUrl);
                      setRecent(loadRecent());
                    }}
                  >
                    forget
                  </span>
                </a>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
