import { describe, expect, it } from "vitest";

import { unpushedNotice } from "./health";

describe("unpushedNotice", () => {
  it("is silent while the server is healthy or unknown", () => {
    expect(unpushedNotice(null)).toBeNull();
    expect(unpushedNotice({ status: "ok", unpushedAgeSeconds: 0 })).toBeNull();
    // A long-unpushed age alone is not a warning: the server decides when
    // it has crossed the threshold.
    expect(unpushedNotice({ status: "ok", unpushedAgeSeconds: 3600 })).toBeNull();
  });

  it("reports the unpushed age in whole minutes when degraded", () => {
    expect(unpushedNotice({ status: "degraded", unpushedAgeSeconds: 600 })).toBe(
      "Changes are saved locally but have not been pushed for 10 minutes",
    );
    expect(unpushedNotice({ status: "degraded", unpushedAgeSeconds: 150 })).toBe(
      "Changes are saved locally but have not been pushed for 3 minutes",
    );
  });

  it("never reports less than one minute, and says it in the singular", () => {
    expect(unpushedNotice({ status: "degraded", unpushedAgeSeconds: 20 })).toBe(
      "Changes are saved locally but have not been pushed for 1 minute",
    );
  });

  it("still warns when the server omits the age", () => {
    expect(unpushedNotice({ status: "degraded" })).toBe(
      "Changes are saved locally but have not been pushed",
    );
  });
});
