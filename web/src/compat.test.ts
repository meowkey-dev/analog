import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type { Health } from "./api";
import { compareVersions, CONTRACT, incompatibility, MIN_SERVER } from "./compat";

function health(fields: Partial<Health>): Health {
  return { ok: true, service: "analog", version: CONTRACT, auth_required: false, ...fields };
}

describe("contract compatibility", () => {
  it("states the contract the repo is frozen at", () => {
    const spec = JSON.parse(readFileSync(
      new URL("../../contracts/openapi.json", import.meta.url), "utf8"));
    expect(CONTRACT).toBe(spec.info.version);
    expect(compareVersions(MIN_SERVER, CONTRACT)).toBeLessThanOrEqual(0);
  });

  it("compares numerically, not as strings", () => {
    expect(compareVersions("0.10.0", "0.9.0")).toBeGreaterThan(0);
    expect(compareVersions("0.8.0", "0.8.0")).toBe(0);
    expect(compareVersions("0.7.9", "0.8.0")).toBeLessThan(0);
  });

  it("accepts a matching server, and an older one inside the window", () => {
    expect(incompatibility(health({ min_client: CONTRACT }), "srv")).toBeNull();
    // before 0.8.0 there is no min_client, and its absence is no minimum
    expect(incompatibility(health({ version: MIN_SERVER }), "srv")).toBeNull();
  });

  it("accepts a newer server that still takes this client", () => {
    expect(incompatibility(health({ version: "9.0.0", min_client: CONTRACT }), "srv")).toBeNull();
  });

  it("refuses a server older than MIN_SERVER", () => {
    expect(incompatibility(health({ version: "0.6.0" }), "srv")).toMatch(/srv is too old/);
  });

  it("refuses when the server demands a newer client", () => {
    expect(incompatibility(health({ version: "9.0.0", min_client: "9.0.0" }), "srv"))
      .toMatch(/This app is too old for srv/);
  });
});
