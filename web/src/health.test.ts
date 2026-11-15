import { describe, expect, it } from "vitest";

import { healthNotice } from "./health";

describe("healthNotice", () => {
  it("is silent while the server is healthy or unknown", () => {
    expect(healthNotice(null)).toBeNull();
    expect(healthNotice({ status: "ok", unpushedAgeSeconds: 0 })).toBeNull();
    // A long-unpushed age alone is not a warning: the server decides when
    // it has crossed the threshold.
    expect(healthNotice({ status: "ok", unpushedAgeSeconds: 3600 })).toBeNull();
  });

  it("reports the unpushed age in whole minutes when degraded", () => {
    expect(healthNotice({ status: "degraded", unpushedAgeSeconds: 600 })).toBe(
      "Changes are saved locally but have not been pushed for 10 minutes",
    );
    expect(healthNotice({ status: "degraded", unpushedAgeSeconds: 150 })).toBe(
      "Changes are saved locally but have not been pushed for 3 minutes",
    );
  });

  it("never reports less than one minute, and says it in the singular", () => {
    expect(healthNotice({ status: "degraded", unpushedAgeSeconds: 20 })).toBe(
      "Changes are saved locally but have not been pushed for 1 minute",
    );
  });

  it("still warns when the server omits the age", () => {
    expect(healthNotice({ status: "degraded" })).toBe(
      "Changes are saved locally but have not been pushed",
    );
  });

  // G90: a server that lost its data directory refuses every change until it
  // is restarted. Saying "saved locally" there would be false: nothing is.
  it("says a lost data directory refuses changes, never that they are saved", () => {
    const lost = {
      status: "degraded",
      unpushedAgeSeconds: 600,
      storeLost: "data directory lost; restart required",
    };
    expect(healthNotice(lost)).toBe(
      "The server lost its data directory: changes are refused until it is restarted",
    );
    expect(healthNotice({ status: "degraded", storeLost: "x" })).not.toContain("saved locally");
  });
});
