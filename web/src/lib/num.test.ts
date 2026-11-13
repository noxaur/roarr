import { describe, expect, it } from "vitest";
import { parseNum } from "./num";

describe("parseNum", () => {
  it("reads a number", () => {
    expect(parseNum("12", 0)).toBe(12);
    expect(parseNum("1.5", 0, 10)).toBe(1.5);
    expect(parseNum(" 7 ", 0)).toBe(7);
    expect(parseNum("1.", 0)).toBe(1);
  });

  it("has no value for an empty or half-typed field", () => {
    for (const text of ["", "  ", "-", ".", "1e", "abc", "Infinity", "-Infinity", "NaN"]) expect(parseNum(text, -10, 10), JSON.stringify(text)).toBeNull();
  });

  it("clamps to min and max", () => {
    expect(parseNum("3", 5)).toBe(5);
    expect(parseNum("-1", 0, 63)).toBe(0);
    expect(parseNum("99", 0, 63)).toBe(63);
    expect(parseNum("1e999", 0, 63)).toBeNull();
    expect(parseNum("1000000", 0)).toBe(1000000);
  });
});
