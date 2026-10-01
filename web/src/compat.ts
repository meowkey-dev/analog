import type { Health } from "./api";

/**
 * Whether this bundle can talk to a given server.
 *
 * The bundle a desktop app shows is the one its own sidecar embeds, and the server
 * it connects to can be any release. Both sides say what they accept — the server
 * through `min_client` on /health, this bundle through MIN_SERVER — so neither has
 * to predict the other's future, and a mismatch is refused at connect time rather
 * than surfacing as whichever operation happened to change.
 */

/** The contract this bundle was written against: contracts/openapi.json info.version. */
export const CONTRACT = "0.8.0";

/**
 * The oldest server contract this bundle still works with. Raise it when the UI
 * starts relying on an operation or field an older server does not have.
 *
 * 0.3.0 brought /health and /whoami, which connecting needs. Nothing since is
 * load-bearing for the UI: 0.4.0 changed only what agents read from feedback,
 * 0.6.0 only what a selector fraction means, and the /upgrade check 0.7.0 added
 * is allowed to fail (Spaces.tsx).
 */
export const MIN_SERVER = "0.3.0";

/** Compare two `major.minor.patch` versions; anything unparsable sorts as 0. */
export function compareVersions(a: string, b: string): number {
  const pa = a.split(".").map((n) => Number.parseInt(n, 10) || 0);
  const pb = b.split(".").map((n) => Number.parseInt(n, 10) || 0);
  for (let i = 0; i < 3; i++) {
    const d = (pa[i] ?? 0) - (pb[i] ?? 0);
    if (d !== 0) return d;
  }
  return 0;
}

/** Why this bundle cannot use the server, or null when it can. */
export function incompatibility(health: Health, where: string): string | null {
  if (compareVersions(health.version, MIN_SERVER) < 0) {
    return `${where} is too old for this app (it speaks API ${health.version}; `
      + `this app needs ${MIN_SERVER} or newer). Upgrade analog-server there.`;
  }
  // Servers before 0.8.0 send no minimum, and accept whatever they used to.
  if (health.min_client && compareVersions(CONTRACT, health.min_client) < 0) {
    return `This app is too old for ${where} (it needs API ${health.min_client}; `
      + `this app speaks ${CONTRACT}). Update the app.`;
  }
  return null;
}
