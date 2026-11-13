import { describe, expect, it } from "vitest";
import { cellCounts } from "./cells";

describe("cellCounts", () => {
  it("draws one cell per episode when they fit", () => {
    expect(cellCounts(3, 10, 24)).toEqual({ cells: 10, have: 3 });
    expect(cellCounts(0, 0, 24)).toEqual({ cells: 1, have: 0 });
    expect(cellCounts(1, 1, 24)).toEqual({ cells: 1, have: 1 });
  });

  it("shows nothing on disk as nothing", () => {
    expect(cellCounts(0, 48, 24)).toEqual({ cells: 24, have: 0 });
  });

  it("shows one episode on disk as a filled cell", () => {
    expect(cellCounts(1, 48, 24)).toEqual({ cells: 24, have: 1 });
  });

  it("never shows an incomplete title as complete", () => {
    expect(cellCounts(47, 48, 24)).toEqual({ cells: 24, have: 23 });
    // 50 episodes in groups of 3 make 17 cells; 49 of them floor to 16 full groups.
    expect(cellCounts(49, 50, 24)).toEqual({ cells: 17, have: 16 });
  });

  it("fills every cell when everything is on disk", () => {
    expect(cellCounts(48, 48, 24)).toEqual({ cells: 24, have: 24 });
    // The last group is short: 50 / 3 floors to 16 of 17.
    expect(cellCounts(50, 50, 24)).toEqual({ cells: 17, have: 17 });
    expect(cellCounts(60, 50, 24)).toEqual({ cells: 17, have: 17 });
  });
});
