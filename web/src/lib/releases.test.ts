import { describe, expect, it } from "vitest";
import type { Release } from "./api";
import { PER_GRAB, WASTE_FACTOR } from "./plan";
import { fitScore } from "./releases";

const rel = (covers: number[]) => ({ score: 100, covers }) as Release;

describe("fitScore", () => {
  it("charges one grab for a release that fills exactly what is wanted", () => {
    expect(fitScore(rel([1]), new Set([1]), "tv")).toBe(100 - PER_GRAB);
  });
  it("charges the planner's waste factor for each episode nobody asked for", () => {
    expect(fitScore(rel([1, 2, 3]), new Set([1]), "tv")).toBeCloseTo(100 - 2 * WASTE_FACTOR * 100 - PER_GRAB);
  });
  it("rejects a release that fills nothing", () => {
    expect(fitScore(rel([2]), new Set([1]), "tv")).toBe(-1);
  });
});
