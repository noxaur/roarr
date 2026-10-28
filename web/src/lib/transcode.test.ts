import { describe, expect, it } from "vitest";
import type { TranscodeJob, TranscodeStatus } from "./api";
import { baseName, jobTag, pendingFor, savedShare } from "./transcode";

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
  });
});
