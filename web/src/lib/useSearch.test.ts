import { describe, expect, it } from "vitest";
import { settleStreams } from "./useSearch";

describe("settleStreams", () => {
  it("is not done while a stream is open", () => {
    expect(settleStreams([])).toEqual({ done: false, errored: false });
    expect(settleStreams(["done", undefined])).toEqual({ done: false, errored: false });
    expect(settleStreams(["error", undefined])).toEqual({ done: false, errored: false });
  });

  it("is done when every stream finished cleanly", () => {
    expect(settleStreams(["done"])).toEqual({ done: true, errored: false });
    expect(settleStreams(["done", "done"])).toEqual({ done: true, errored: false });
  });

  it("is errored, not done, when the stream ended in an error", () => {
    expect(settleStreams(["error"])).toEqual({ done: false, errored: true });
  });

  it("is errored, not done, when one of several streams ended in an error", () => {
    expect(settleStreams(["done", "error"])).toEqual({ done: false, errored: true });
  });
});
