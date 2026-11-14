import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, api, setUnauthorizedHandler } from "./api";

const answer = (status: number, body?: unknown, headers?: Record<string, string>) =>
  vi.fn(async (_url: string, _init?: RequestInit) => new Response(body === undefined ? null : JSON.stringify(body), { status, headers }));
const stub = (f: ReturnType<typeof answer>) => {
  vi.stubGlobal("fetch", f);
  return f;
};

afterEach(() => {
  vi.unstubAllGlobals();
  setUnauthorizedHandler(() => {});
});

describe("api.library", () => {
  const body = { items: [], errors: { sonarr: "down" } };

  it("returns the library with its ETag", async () => {
    const f = stub(answer(200, body, { ETag: '"v2"' }));
    expect(await api.library('"v1"')).toEqual({ ...body, etag: '"v2"' });
    expect(f.mock.calls[0][0]).toBe("/api/library");
    expect(new Headers(f.mock.calls[0][1]?.headers).get("If-None-Match")).toBe('"v1"');
  });

  it("returns null when the server says the copy is current", async () => {
    stub(answer(304));
    expect(await api.library('"v1"')).toBeNull();
  });

  it("asks for a rebuild without a condition", async () => {
    const f = stub(answer(200, body));
    await api.library('"v1"', true);
    expect(f.mock.calls[0][0]).toBe("/api/library?refresh=1");
    expect(new Headers(f.mock.calls[0][1]?.headers).has("If-None-Match")).toBe(false);
  });

  it("throws on a failure, so the caller can show it", async () => {
    stub(answer(502, { error: "sonarr unreachable" }));
    await expect(api.library(null)).rejects.toEqual(new ApiError(502, "sonarr unreachable"));
  });

  it("runs the shared sign-out handler on 401", async () => {
    const onUnauthorized = vi.fn();
    setUnauthorizedHandler(onUnauthorized);
    stub(answer(401, { error: "sign in" }));
    await expect(api.library(null)).rejects.toMatchObject({ status: 401 });
    expect(onUnauthorized).toHaveBeenCalledOnce();
  });
});
