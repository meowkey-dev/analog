// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { api, isAnalogMediaUrl, setConnection } from "./api";

describe("authenticated media URLs", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    setConnection({ baseUrl: "", token: null });
  });

  it("recognizes only the configured server's Analog media route", () => {
    setConnection({ baseUrl: "https://analog.example", token: "secret" });
    expect(isAnalogMediaUrl("https://analog.example/api/spaces/redesign/media/m_1.png")).toBe(true);
    expect(isAnalogMediaUrl("https://cdn.example/api/spaces/redesign/media/m_1.png")).toBe(false);
    expect(isAnalogMediaUrl("https://analog.example/api/spaces/redesign/canvas")).toBe(false);
    expect(isAnalogMediaUrl("https://analog.example/api/spaces/redesign/media/../canvas")).toBe(false);
  });

  it("sends the bearer only to same-origin Analog media", async () => {
    setConnection({ baseUrl: "https://analog.example", token: "secret" });
    const createObjectURL = vi.fn(() => "blob:object");
    const hadCreateObjectURL = Object.prototype.hasOwnProperty.call(URL, "createObjectURL");
    const previousCreateObjectURL = URL.createObjectURL;
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: createObjectURL });
    const calls: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ input, init });
      return new Response(new Blob(["media"], { type: "image/png" }), {
        status: 200,
        headers: { "content-type": "image/png" },
      });
    }));

    try {
      await api.mediaObjectUrl("/api/spaces/redesign/media/m_1.png");
      await api.mediaObjectUrl("https://cdn.example/image.png");
    } finally {
      if (hadCreateObjectURL) {
        Object.defineProperty(URL, "createObjectURL", {
          configurable: true, value: previousCreateObjectURL,
        });
      } else {
        Reflect.deleteProperty(URL, "createObjectURL");
      }
    }

    expect(calls[0]?.init?.credentials).toBe("omit");
    expect(calls[0]?.init?.headers).toEqual({ authorization: "Bearer secret" });
    expect(calls[1]?.init?.credentials).toBe("omit");
    expect(calls[1]?.init?.headers).toEqual({});
    expect(createObjectURL).toHaveBeenCalledTimes(2);
  });
});
