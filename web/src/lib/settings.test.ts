import { describe, expect, it } from "vitest";
import type { VerifySettings } from "./api";
import { badPathPairs, countChanges, setVerifyMode, verifyMode } from "./settings";

describe("verify mode", () => {
  it("reads the tri-state", () => {
    expect(verifyMode({ autoRegrab: false })).toBe("auto");
    expect(verifyMode({ autoRegrab: false, enabled: null })).toBe("auto");
    expect(verifyMode({ autoRegrab: false, enabled: true })).toBe("on");
    expect(verifyMode({ autoRegrab: false, enabled: false })).toBe("off");
  });
  it("auto drops the key so the server sees it unset, and keeps the other fields", () => {
    const v: VerifySettings = { enabled: false, autoRegrab: true, pathMap: "/a=/b" };
    setVerifyMode(v, "auto");
    expect(JSON.parse(JSON.stringify(v))).toEqual({ autoRegrab: true, pathMap: "/a=/b" });
    setVerifyMode(v, "off");
    expect(v).toEqual({ autoRegrab: true, pathMap: "/a=/b", enabled: false });
  });
});

describe("badPathPairs", () => {
  it("accepts well-formed maps and blanks", () => {
    expect(badPathPairs(undefined)).toEqual([]);
    expect(badPathPairs("")).toEqual([]);
    expect(badPathPairs("/downloads=/data/downloads, D:\\Torrents=/torrents,")).toEqual([]);
  });
  it("names the entries the server ignores", () => {
    expect(badPathPairs("/a=/b,/c,=/d,/e=")).toEqual(["/c", "=/d", "/e="]);
  });
});

describe("countChanges", () => {
  it("counts leaves, including a key going from unset to false", () => {
    const a = { verify: { autoRegrab: false }, import: { mode: "arr" } };
    const b = { verify: { autoRegrab: false, enabled: false }, import: { mode: "native" } };
    expect(countChanges(a, b)).toBe(2);
    expect(countChanges(a, structuredClone(a))).toBe(0);
  });
});
