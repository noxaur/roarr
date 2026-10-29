import { describe, expect, it } from "vitest";
import type { TranscodeJob, TranscodeStatus } from "./api";
import { baseName, holdText, jobTag, pendingFor, savedShare, serverClock } from "./transcode";

const job = (over: Partial<TranscodeJob>): TranscodeJob => ({ id: "j", path: "/media/tv/Show/Season 01/Show - S01E01.mkv", state: "queued", percent: 0, queuedAt: "", kind: "tv", titleId: 1, ...over });
const status = (jobs: TranscodeJob[], enabled = true): TranscodeStatus => ({
  enabled,
  available: true,
  concurrency: 1,
  counts: { queued: 0, running: 0, done: 0, skipped: 0, failed: 0 },
  bytesSaved: 0,
  jobs,
});

describe("transcode", () => {
  it("takes the file name from either slash", () => {
    expect(baseName("/a/b/c.mkv")).toBe("c.mkv");
    expect(baseName("D:\\x\\y.mkv")).toBe("y.mkv");
    expect(baseName("z.mkv")).toBe("z.mkv");
  });
  it("keeps the pending jobs of one title, by file name", () => {
    const st = status([
      job({ id: "1", state: "running", percent: 41.7 }),
      job({ id: "2", path: "/m/Show - S01E02.mkv", state: "done" }),
      job({ id: "3", path: "/m/Other - S01E01.mkv", titleId: 2 }),
      job({ id: "4", path: "/m/Movie.mkv", kind: "movie" }),
    ]);
    const m = pendingFor(st, "tv", 1);
    expect([...m.keys()]).toEqual(["Show - S01E01.mkv"]);
    expect(jobTag(m.get("Show - S01E01.mkv")!)).toBe("AV1 41%");
    expect(pendingFor(status(st.jobs, false), "tv", 1).size).toBe(0);
    expect(pendingFor(null, "tv", 1).size).toBe(0);
  });
  it("tags queued jobs and computes the saved share", () => {
    expect(jobTag(job({}))).toBe("AV1 queued");
    expect(savedShare(job({ srcBytes: 1000, savedBytes: 540 }))).toBeCloseTo(0.54);
    expect(savedShare(job({}))).toBe(0);
    expect(jobTag(job({ state: "running", percent: 50, paused: true }))).toBe("AV1 paused");
  });
  it("shows schedule times on the server's clock", () => {
    const now = new Date("2026-11-02T12:00:00Z"); // Monday
    expect(serverClock("2026-11-02T21:00:00Z", 3600, now)).toBe("22:00");
    expect(serverClock("2026-11-03T06:00:00Z", 3600, now)).toBe("Tue 07:00");
    expect(serverClock("2026-11-02T23:30:00Z", 0, now)).toBe("23:30");
  });
  it("says what holds the queue", () => {
    const now = new Date("2026-11-02T12:00:00Z");
    const st = (over: Partial<TranscodeStatus>) => ({ ...status([]), ...over });
    expect(holdText(st({}), now)).toBe("");
    expect(holdText(st({ paused: true, held: true, scheduled: true, nextChange: "2026-11-02T22:00:00Z" }), now)).toBe("Paused");
    expect(holdText(st({ held: true, scheduled: true, nextChange: "2026-11-02T22:00:00Z", zoneOffset: 0 }), now)).toBe("Waiting until 22:00");
    expect(holdText(st({ scheduled: true, nextChange: "2026-11-03T07:00:00Z", zoneOffset: 0 }), now)).toBe("Running until Tue 07:00");
    expect(holdText(st({ enabled: false, paused: true }), now)).toBe("");
  });
});
