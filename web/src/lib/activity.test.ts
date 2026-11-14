import { describe, expect, it } from "vitest";
import { isActive, needsYou, statusText } from "./activity";
import type { ActivityItem } from "./api";

const item = (status: string) => ({ status }) as ActivityItem;
// Every status internal/app sends (ActivityItem.Status).
const STATUSES = ["downloading", "queued", "paused", "importing", "warning", "failed", "stalled"];

describe("download statuses", () => {
  it("puts every status in exactly one bucket", () => {
    for (const s of STATUSES) expect([s, isActive(item(s)) !== needsYou(item(s))]).toEqual([s, true]);
  });

  it("asks for the user when a download stalled", () => {
    expect(needsYou(item("stalled"))).toBe(true);
    expect(statusText("stalled")).toBe("Stalled");
  });

  it("keeps a paused download in the list without asking for the user", () => {
    expect(isActive(item("paused"))).toBe(true);
    expect(needsYou(item("paused"))).toBe(false);
    expect(statusText("paused")).toBe("Paused");
  });

  it("labels every status but a plain download", () => {
    for (const s of STATUSES) expect([s, statusText(s) !== ""]).toEqual([s, s !== "downloading"]);
  });
});
