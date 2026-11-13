import { afterEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { copyText } from "./copy";

vi.mock("sonner", () => ({ toast: Object.assign(vi.fn(), { error: vi.fn() }) }));

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe("copyText", () => {
  it("confirms a copy", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    await copyText("Some.Release-GRP", "Copied release name");
    expect(writeText).toHaveBeenCalledWith("Some.Release-GRP");
    expect(toast).toHaveBeenCalledWith("Copied release name");
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("reports a refused copy", async () => {
    vi.stubGlobal("navigator", { clipboard: { writeText: vi.fn().mockRejectedValue(new Error("Document is not focused")) } });
    await copyText("x", "Copied");
    expect(toast).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith("Could not copy", { description: "Document is not focused" });
  });

  it("reports a browser without a clipboard (plain http)", async () => {
    vi.stubGlobal("navigator", {});
    await copyText("x", "Copied");
    expect(toast).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith("Could not copy", { description: "The browser only allows copying over HTTPS or on localhost." });
  });
});
