import { describe, expect, it } from "vitest";

import { queryString, viewQuery } from "./viewquery";

describe("viewQuery", () => {
  it("scopes the Me board to the day, with reviews and no user or team", () => {
    const q = viewQuery("me", "2026-07-04", ["alpha", "beta"]);
    expect(q).toEqual({ view: "me", day: "2026-07-04", reviews: "true" });
    expect(q.user).toBeUndefined();
    expect(q.team).toBeUndefined();
  });

  it("names every team the Team board shows as a comma set", () => {
    const q = viewQuery("team", "2026-07-04", ["alpha", "beta"]);
    expect(q).toEqual({
      view: "team",
      day: "2026-07-04",
      team: "alpha,beta",
      reviews: "true",
    });
  });

  it("sends an empty team set when the Team board shows no teams", () => {
    expect(viewQuery("team", "2026-07-04", []).team).toBe("");
  });
});

describe("queryString", () => {
  it("serialises with a stable (sorted) key order and encodes values", () => {
    const q = viewQuery("team", "2026-07-04", ["a b", "c"]);
    expect(queryString(q)).toBe(
      "day=2026-07-04&reviews=true&team=a%20b%2Cc&view=team",
    );
  });

  it("is identical for equal selectors, so the watch does not re-subscribe", () => {
    const a = queryString(viewQuery("me", "2026-07-04", []));
    const b = queryString(viewQuery("me", "2026-07-04", ["ignored-for-me"]));
    expect(a).toBe(b);
  });
});
