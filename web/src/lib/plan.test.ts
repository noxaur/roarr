import { describe, expect, it } from "vitest";
import { planEpisodes, type Option } from "./plan";

const seq = (a: number, b: number) => Array.from({ length: b - a + 1 }, (_, i) => a + i);

// Mirrors internal/release/plan_test.go.
describe("planEpisodes", () => {
  it("prefers a pack on a tie", () => {
    const opts: Option[] = [{ key: "pack", score: 70, rejected: false, covers: seq(1, 10) }];
    for (let e = 1; e <= 10; e++) opts.push({ key: `s${e}`, score: 72, rejected: false, covers: [e] });
    const p = planEpisodes(seq(1, 10), opts);
    expect(p.picks.map((x) => x.key)).toEqual(["pack"]);
  });

  it("prefers clearly better singles", () => {
    const opts: Option[] = [{ key: "pack720", score: 50, rejected: false, covers: seq(1, 10) }];
    for (let e = 1; e <= 10; e++) opts.push({ key: `s${e}`, score: 75, rejected: false, covers: [e] });
    expect(planEpisodes(seq(1, 10), opts).picks).toHaveLength(10);
  });

  it("avoids a pack for a single episode", () => {
    const p = planEpisodes([3], [
      { key: "pack", score: 80, rejected: false, covers: seq(1, 10) },
      { key: "e3", score: 70, rejected: false, covers: [3] },
    ]);
    expect(p.picks.map((x) => x.key)).toEqual(["e3"]);
  });

  it("fills gaps and reports missing", () => {
    const p = planEpisodes(seq(1, 13), [
      { key: "s1", score: 70, rejected: false, covers: seq(1, 10) },
      { key: "e12", score: 60, rejected: false, covers: [12] },
      { key: "bad", score: 90, rejected: true, covers: seq(11, 20) },
    ]);
    expect(p.picks.map((x) => x.key)).toEqual(["s1", "e12"]);
    expect(p.missing).toEqual([11, 13]);
  });
});
