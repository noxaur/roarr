import { describe, expect, it } from "vitest";
import { ARR_KEEPERS, appName, appNames } from "./keeper";

describe("appName", () => {
  it("names the app that keeps each kind", () => {
    expect(appName(ARR_KEEPERS, "tv")).toBe("Sonarr");
    expect(appName(ARR_KEEPERS, "movie")).toBe("Radarr");
    expect(appName({ tv: "roarr", movie: "radarr" }, "tv")).toBe("roarr");
  });
});

describe("appNames", () => {
  it("joins the distinct apps", () => {
    expect(appNames(ARR_KEEPERS)).toBe("Sonarr and Radarr");
    expect(appNames(ARR_KEEPERS, ["movie"])).toBe("Radarr");
    expect(appNames({ tv: "roarr", movie: "roarr" })).toBe("roarr");
    expect(appNames({ tv: "roarr", movie: "radarr" })).toBe("roarr and Radarr");
  });
});
