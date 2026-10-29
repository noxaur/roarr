import { describe, expect, it, vi } from "vitest";
import { age, cached, forget, peek, put } from "./cache";

describe("cached", () => {
  it("shares one request between callers and keeps the answer", async () => {
    const load = vi.fn(() => Promise.resolve(1));
    const [a, b] = await Promise.all([cached("a", load, 1000), cached("a", load, 1000)]);
    expect([a, b]).toEqual([1, 1]);
    expect(load).toHaveBeenCalledTimes(1);
    expect(peek("a")).toBe(1);
    await cached("a", load, 1000);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("loads again when the value is older than maxAge, and keeps the old value meanwhile", async () => {
    put("b", "old");
    let resolve!: (v: string) => void;
    const p = cached("b", () => new Promise<string>((r) => (resolve = r)), -1);
    expect(peek("b")).toBe("old");
    resolve("new");
    expect(await p).toBe("new");
    expect(peek("b")).toBe("new");
    expect(age("b")).toBeLessThan(100);
  });

  it("keeps the last good value when a reload fails, and forgets a failed first load", async () => {
    put("c", "good");
    await expect(cached("c", () => Promise.reject(new Error("x")), -1)).rejects.toThrow("x");
    expect(peek("c")).toBe("good");
    await expect(cached("d", () => Promise.reject(new Error("x")))).rejects.toThrow("x");
    expect(peek("d")).toBeUndefined();
    expect(age("d")).toBe(Infinity);
  });

  it("forgets by prefix", () => {
    put("discover:a", 1);
    put("discover:b", 2);
    put("tv:1", 3);
    forget("discover:");
    expect(peek("discover:a")).toBeUndefined();
    expect(peek("tv:1")).toBe(3);
  });
});
