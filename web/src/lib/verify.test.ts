import { describe, expect, it } from "vitest";
import type { ActivityItem, VerifyCheck, VerifyRecord } from "./api";
import { byHash, forActivity, forEpisode, forMovie, headline, isBlocking, isFlagged, orphans, outcome, problems } from "./verify";

const rec = (over: Partial<VerifyRecord>): VerifyRecord => ({
  hash: "aa",
  source: "sonarr",
  kind: "tv",
  title: "Show",
  release: "Show.S01E01",
  added: "2026-10-01T00:00:00Z",
  state: "done",
  ...over,
});
const verdict = (level: "pass" | "warn" | "fail" | "skip", checks: VerifyCheck[] = []) => ({
  level,
  summary: `summary ${level}`,
  checks,
  at: "2026-10-01T00:00:00Z",
  tookMs: 1,
});
const act = (downloadId?: string): ActivityItem => ({ id: "x", source: "sonarr", kind: "tv", title: "Show", subtitle: "", release: "", status: "downloading", progress: 0, size: 0, sizeLeft: 0, downloadId });

describe("outcome", () => {
  it("asks for a decision on an undismissed failure", () => {
    expect(outcome(rec({ verdict: verdict("fail") }))).toBe("fail");
    expect(isBlocking(rec({ verdict: verdict("fail") }))).toBe(true);
  });
  it("flags a warning without blocking", () => {
    const r = rec({ verdict: verdict("warn") });
    expect(outcome(r)).toBe("warn");
    expect(isFlagged(r)).toBe(true);
    expect(isBlocking(r)).toBe(false);
  });
  it("is settled once kept or re-grabbed", () => {
    expect(outcome(rec({ verdict: verdict("fail"), dismissed: true }))).toBe("ok");
    expect(outcome(rec({ verdict: verdict("fail"), rejected: true, regrab: { at: "", auto: false, message: "ok" } }))).toBe("ok");
  });
  it("blocks when the re-grab failed, and shows its error", () => {
    const r = rec({ verdict: verdict("fail"), rejected: true, regrab: { at: "", auto: true, message: "", error: "no release left" } });
    expect(outcome(r)).toBe("regrabFailed");
    expect(isBlocking(r)).toBe(true);
    expect(headline(r)).toBe("no release left");
  });
  it("waits while not judged; pass and skip need nothing", () => {
    expect(outcome(rec({ state: "checking" }))).toBe("checking");
    expect(outcome(rec({ state: "done" }))).toBe("checking");
    expect(outcome(rec({ verdict: verdict("pass") }))).toBe("ok");
    expect(outcome(rec({ verdict: verdict("skip") }))).toBe("ok");
  });
});

describe("problems", () => {
  it("lists failures first, then warnings, in their own order, without passes", () => {
    const r = rec({
      verdict: verdict("fail", [
        { name: "payload", level: "pass", reason: "ok" },
        { name: "audio", level: "warn", reason: "a" },
        { name: "picture", level: "fail", reason: "b" },
        { name: "subtitles", level: "warn", reason: "c" },
        { name: "duration", level: "fail", reason: "d" },
        { name: "hdr", level: "skip", reason: "e" },
      ]),
    });
    expect(problems(r).map((c) => c.name)).toEqual(["picture", "duration", "audio", "subtitles"]);
  });
  it("copes with no checks", () => {
    expect(problems(rec({ verdict: { ...verdict("fail"), checks: null } }))).toEqual([]);
    expect(problems(rec({}))).toEqual([]);
  });
});

describe("matching", () => {
  it("finds a queue item's record by download id, case-insensitively", () => {
    const idx = byHash([rec({ hash: "abcdef" })]);
    expect(forActivity(idx, act("ABCDEF"))?.hash).toBe("abcdef");
    expect(forActivity(idx, act())).toBeUndefined();
  });
  it("takes the newest record that covers an episode", () => {
    const rs = [
      rec({ hash: "old", seriesId: 1, episodeIds: [10, 11], added: "2026-10-01T00:00:00Z" }),
      rec({ hash: "new", seriesId: 1, episodeIds: [11], added: "2026-10-05T00:00:00Z" }),
      rec({ hash: "other", seriesId: 2, episodeIds: [11], added: "2026-10-09T00:00:00Z" }),
    ];
    expect(forEpisode(rs, 1, 11)?.hash).toBe("new");
    expect(forEpisode(rs, 1, 10)?.hash).toBe("old");
    expect(forEpisode(rs, 1, 12)).toBeUndefined();
  });
  it("takes the newest record of a movie", () => {
    const rs = [
      rec({ hash: "a", kind: "movie", movieId: 5, added: "2026-10-05T00:00:00Z" }),
      rec({ hash: "b", kind: "movie", movieId: 5, added: "2026-10-01T00:00:00Z" }),
      rec({ hash: "c", kind: "tv", seriesId: 5 }),
    ];
    expect(forMovie(rs, 5)?.hash).toBe("a");
  });
  it("lists blocking records that left the queue", () => {
    const rs = [
      rec({ hash: "inqueue", verdict: verdict("fail") }),
      rec({ hash: "gone", verdict: verdict("fail") }),
      rec({ hash: "warned", verdict: verdict("warn") }),
      rec({ hash: "kept", verdict: verdict("fail"), dismissed: true }),
    ];
    expect(orphans(rs, [act("INQUEUE")]).map((r) => r.hash)).toEqual(["gone"]);
  });
});
